package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// fakeIssuer is an in-process OIDC provider: it serves a discovery doc, a JWKS,
// and a token endpoint that mints signed id_tokens. It drives the real
// *oidc.OIDCProvider without any external dependency.
type fakeIssuer struct {
	t        *testing.T
	srv      *httptest.Server
	key      *rsa.PrivateKey
	signer   jose.Signer
	keyID    string
	clientID string
	// idTokenClaims is the claim set returned by the token endpoint.
	idTokenClaims map[string]any
	// tokenStatus overrides the token endpoint HTTP status when non-zero.
	tokenStatus int
	// omitIDToken drops the id_token from the token response when true.
	omitIDToken bool
	// emptyIDToken sets the id_token to "" when true.
	emptyIDToken bool
	// badDiscovery serves a malformed discovery document when true.
	badDiscovery bool
}

func newFakeIssuer(t *testing.T, clientID string) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const kid = "test-key-1"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	require.NoError(t, err)

	iss := &fakeIssuer{t: t, key: key, signer: signer, keyID: kid, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		if iss.badDiscovery {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{not json`))
			return
		}
		base := iss.srv.URL
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                base,
			"authorization_endpoint":                base + "/auth",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     kid,
			Algorithm: "RS256",
			Use:       "sig",
		}}}
		_ = json.NewEncoder(w).Encode(jwks)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if iss.tokenStatus != 0 {
			w.WriteHeader(iss.tokenStatus)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		resp := map[string]any{
			"access_token": "fake-access",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		if !iss.omitIDToken {
			if iss.emptyIDToken {
				resp["id_token"] = ""
			} else {
				claims := iss.idTokenClaims
				if claims == nil {
					claims = map[string]any{}
				}
				resp["id_token"] = iss.sign(claims)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	iss.srv = httptest.NewServer(mux)
	t.Cleanup(iss.srv.Close)
	return iss
}

// sign returns a compact-serialized JWT for the given claims. Default iss/aud/
// exp/iat are injected when absent so the go-oidc verifier accepts them.
func (iss *fakeIssuer) sign(claims map[string]any) string {
	iss.t.Helper()
	c := map[string]any{}
	for k, v := range claims {
		c[k] = v
	}
	if _, ok := c["iss"]; !ok {
		c["iss"] = iss.srv.URL
	}
	if _, ok := c["aud"]; !ok {
		c["aud"] = iss.clientID
	}
	if _, ok := c["exp"]; !ok {
		c["exp"] = time.Now().Add(time.Hour).Unix()
	}
	if _, ok := c["iat"]; !ok {
		c["iat"] = time.Now().Unix()
	}
	if _, ok := c["sub"]; !ok {
		c["sub"] = "default-sub"
	}
	payload, err := json.Marshal(c)
	require.NoError(iss.t, err)
	jws, err := iss.signer.Sign(payload)
	require.NoError(iss.t, err)
	tok, err := jws.CompactSerialize()
	require.NoError(iss.t, err)
	return tok
}

func (iss *fakeIssuer) URL() string { return iss.srv.URL }

func baseConfig(iss *fakeIssuer) oidc.Config {
	return oidc.Config{
		Slug:         "authentik",
		DisplayName:  "Authentik",
		IssuerURL:    iss.URL(),
		ClientID:     iss.clientID,
		ClientSecret: "secret",
		RedirectURL:  "https://api.example.com/oauth/callback",
	}
}

// === New: validation error paths ===

func TestNew_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		cfg  oidc.Config
		want string
	}{
		{"missing slug", oidc.Config{}, "slug is required"},
		{"missing issuer", oidc.Config{Slug: "s"}, "issuer URL is required"},
		{"missing client id", oidc.Config{Slug: "s", IssuerURL: "https://x"}, "client ID is required"},
		{"missing client secret", oidc.Config{Slug: "s", IssuerURL: "https://x", ClientID: "c"}, "client secret is required"},
		{"missing redirect", oidc.Config{Slug: "s", IssuerURL: "https://x", ClientID: "c", ClientSecret: "k"}, "redirect URL is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := oidc.New(context.Background(), tt.cfg)
			require.Error(t, err)
			assert.Nil(t, p)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestNew_DiscoveryError(t *testing.T) {
	iss := newFakeIssuer(t, "client-x")
	iss.badDiscovery = true
	cfg := baseConfig(iss)
	cfg.ClientID = "client-x"
	p, err := oidc.New(context.Background(), cfg)
	require.Error(t, err)
	assert.Nil(t, p)
	assert.Contains(t, err.Error(), "discovery")
}

func TestNew_Success_WithProvidedScopesAndDisplayName(t *testing.T) {
	iss := newFakeIssuer(t, "client-x")
	cfg := baseConfig(iss)
	cfg.ClientID = "client-x"
	// Scopes provided but missing openid -> openid is prepended.
	cfg.Scopes = []string{"email", "profile"}
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "authentik", p.Name())
	assert.Equal(t, "Authentik", p.DisplayName())

	// The built auth URL should carry the openid scope (prepended).
	au, err := p.BuildAuthURL(context.Background(), "state-1")
	require.NoError(t, err)
	parsed, err := url.Parse(au.URL)
	require.NoError(t, err)
	scope := parsed.Query().Get("scope")
	assert.Contains(t, scope, gooidc.ScopeOpenID)
	assert.Contains(t, scope, "email")
}

func TestNew_DefaultsScopesAndDisplayName(t *testing.T) {
	iss := newFakeIssuer(t, "client-x")
	cfg := baseConfig(iss)
	cfg.ClientID = "client-x"
	cfg.DisplayName = "" // falls back to slug
	cfg.Scopes = nil     // falls back to default openid/email/profile
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "authentik", p.DisplayName())

	au, err := p.BuildAuthURL(context.Background(), "state-2")
	require.NoError(t, err)
	parsed, err := url.Parse(au.URL)
	require.NoError(t, err)
	scope := parsed.Query().Get("scope")
	for _, want := range []string{gooidc.ScopeOpenID, "email", "profile"} {
		assert.Contains(t, scope, want)
	}
}

func TestNew_ScopesAlreadyContainOpenID(t *testing.T) {
	iss := newFakeIssuer(t, "client-x")
	cfg := baseConfig(iss)
	cfg.ClientID = "client-x"
	cfg.Scopes = []string{gooidc.ScopeOpenID, "email"}
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	au, err := p.BuildAuthURL(context.Background(), "state-3")
	require.NoError(t, err)
	parsed, err := url.Parse(au.URL)
	require.NoError(t, err)
	// openid should not be duplicated.
	scope := parsed.Query().Get("scope")
	assert.Equal(t, 1, strings.Count(scope, gooidc.ScopeOpenID))
}

// === BuildAuthURL ===

func TestBuildAuthURL_CarriesPKCEAndNonce(t *testing.T) {
	iss := newFakeIssuer(t, "client-x")
	cfg := baseConfig(iss)
	cfg.ClientID = "client-x"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	au, err := p.BuildAuthURL(context.Background(), "the-state")
	require.NoError(t, err)
	require.NotNil(t, au)
	assert.NotEmpty(t, au.PKCEVerifier)
	assert.NotEmpty(t, au.Nonce)

	parsed, err := url.Parse(au.URL)
	require.NoError(t, err)
	q := parsed.Query()
	assert.Equal(t, "the-state", q.Get("state"))
	assert.Equal(t, "S256", q.Get("code_challenge_method"))
	assert.NotEmpty(t, q.Get("code_challenge"))
	assert.Equal(t, au.Nonce, q.Get("nonce"))
}

// === Exchange ===

func newProvider(t *testing.T, iss *fakeIssuer) *oidc.OIDCProvider {
	t.Helper()
	cfg := baseConfig(iss)
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)
	return p
}

func TestExchange_Success_NameFromName(t *testing.T) {
	iss := newFakeIssuer(t, "authentik-client")
	iss.clientID = "authentik-client"
	cfg := baseConfig(iss)
	cfg.ClientID = "authentik-client"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.idTokenClaims = map[string]any{
		"sub":            "subject-123",
		"email":          "User@Example.COM",
		"email_verified": true,
		"name":           "Full Name",
		"nonce":          "nonce-abc",
	}
	ui, err := p.Exchange(context.Background(), "auth-code", "verifier", "nonce-abc")
	require.NoError(t, err)
	require.NotNil(t, ui)
	assert.Equal(t, "authentik", ui.Provider)
	assert.Equal(t, "subject-123", ui.ExternalSubject)
	assert.Equal(t, "user@example.com", ui.Email) // lowercased
	assert.True(t, ui.EmailVerified)
	assert.Equal(t, "Full Name", ui.DisplayName)
	assert.Equal(t, "subject-123", ui.Raw["sub"])
}

func TestExchange_NameFallsBackToPreferredUsername(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.idTokenClaims = map[string]any{
		"sub":                "s1",
		"email":              "a@b.com",
		"preferred_username": "preferred",
		"nonce":              "n",
	}
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.NoError(t, err)
	assert.Equal(t, "preferred", ui.DisplayName)
}

func TestExchange_NameFallsBackToEmail(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.idTokenClaims = map[string]any{
		"sub":   "s1",
		"email": "fallback@b.com",
		"nonce": "n",
	}
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.NoError(t, err)
	assert.Equal(t, "fallback@b.com", ui.DisplayName)
}

func TestExchange_TokenEndpointError(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.tokenStatus = http.StatusBadRequest
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "token exchange")
}

func TestExchange_NoIDToken(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.omitIDToken = true
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "no id_token")
}

func TestExchange_EmptyIDToken(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.emptyIDToken = true
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "no id_token")
}

func TestExchange_VerifyError_WrongAudience(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	// aud mismatches the configured client ID -> verifier rejects.
	iss.idTokenClaims = map[string]any{
		"sub":   "s1",
		"aud":   "some-other-client",
		"nonce": "n",
	}
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "verify id_token")
}

func TestExchange_ClaimsDecodeError(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	// email_verified is typed bool in the decode struct; supplying a string
	// makes idt.Claims(&claims) fail to unmarshal.
	iss.idTokenClaims = map[string]any{
		"sub":            "s1",
		"email":          "a@b.com",
		"email_verified": "not-a-bool",
		"nonce":          "n",
	}
	ui, err := p.Exchange(context.Background(), "code", "v", "n")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "decode claims")
}

func TestExchange_NonceMismatch(t *testing.T) {
	iss := newFakeIssuer(t, "c")
	iss.clientID = "c"
	cfg := baseConfig(iss)
	cfg.ClientID = "c"
	p, err := oidc.New(context.Background(), cfg)
	require.NoError(t, err)

	iss.idTokenClaims = map[string]any{
		"sub":   "s1",
		"email": "a@b.com",
		"nonce": "actual-nonce",
	}
	ui, err := p.Exchange(context.Background(), "code", "v", "expected-nonce")
	require.Error(t, err)
	assert.Nil(t, ui)
	assert.Contains(t, err.Error(), "nonce mismatch")
}
