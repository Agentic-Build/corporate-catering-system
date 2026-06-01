package hydra_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/require"
)

// oidcIssuer is an in-process fake OIDC provider (Hydra-or-Authentik shaped):
// it serves a discovery doc, a JWKS, and a token endpoint that mints signed
// id_tokens. Used to drive the real *oidc.OIDCProvider and the
// *hydra.AccessTokenVerifier without any external dependency.
type oidcIssuer struct {
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
}

func newOIDCIssuer(t *testing.T, clientID string) *oidcIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	const kid = "test-key-1"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid),
	)
	require.NoError(t, err)

	iss := &oidcIssuer{t: t, key: key, signer: signer, keyID: kid, clientID: clientID}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
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
		claims := iss.idTokenClaims
		if claims == nil {
			claims = map[string]any{}
		}
		idToken := iss.sign(claims)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"id_token":     idToken,
		})
	})
	iss.srv = httptest.NewServer(mux)
	t.Cleanup(iss.srv.Close)
	return iss
}

// sign returns a compact-serialized JWT for the given claims. Default iss/aud/
// exp/iat are injected when absent so the go-oidc verifier accepts them.
func (iss *oidcIssuer) sign(claims map[string]any) string {
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

func (iss *oidcIssuer) URL() string { return iss.srv.URL }
