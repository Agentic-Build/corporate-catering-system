package hydra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// AccessTokenVerifier validates JWT access tokens issued by Hydra. The
// issuer is Hydra's public URL (e.g. http://localhost:4444/); the audience
// is set per-client via the OAuth2 token request — Hydra picks one of the
// requested resources, or none, depending on configuration.
//
// We use go-oidc's verifier even though it's named for ID tokens — under the
// hood it just resolves JWKS via OIDC discovery and validates the signature
// and the standard claims, which is exactly what we need for access tokens
// too. SkipClientIDCheck=true because access tokens don't carry our client
// ID (they're issued to many possible clients via DCR).
type AccessTokenVerifier struct {
	verifier *gooidc.IDTokenVerifier
	issuer   string
	// expectedAudience is this resource server's identifier (the MCP URL).
	// When set, a token carrying an audience must include it. Empty disables
	// the check.
	expectedAudience string
}

// NewAccessTokenVerifier resolves Hydra's discovery document at boot and
// caches the resulting JWKS-backed verifier.
//
//   - reachableURL is the URL the Go process can hit to reach Hydra (e.g.
//     http://hydra:4444 inside docker, http://localhost:4444 from the host).
//   - publicIssuer is the URL Hydra advertises as the issuer in the JWTs
//     it signs (set via URLS_SELF_ISSUER). When the API reverse-proxies
//     Hydra under its own host this differs from reachableURL — the JWT
//     iss claim is "http://localhost:8080/" while the OIDC discovery
//     fetch goes to "http://localhost:4444". InsecureIssuerURLContext is
//     go-oidc's mechanism for that exact mismatch.
//
// When publicIssuer is empty we fall back to single-URL mode (no proxy).
//   - expectedAudience is this resource server's identifier (the MCP URL).
//     When non-empty, a token that carries an audience must include it; see
//     audienceAllowed for the exact (DCR-safe) semantics.
func NewAccessTokenVerifier(ctx context.Context, reachableURL, publicIssuer, expectedAudience string) (*AccessTokenVerifier, error) {
	if reachableURL == "" {
		return nil, errors.New("hydra: reachable URL is required")
	}
	if publicIssuer != "" {
		ctx = gooidc.InsecureIssuerURLContext(ctx, publicIssuer)
	}
	provider, err := gooidc.NewProvider(ctx, reachableURL)
	if err != nil {
		return nil, fmt.Errorf("hydra oidc discovery (%s): %w", reachableURL, err)
	}
	iss := publicIssuer
	if iss == "" {
		iss = reachableURL
	}
	return &AccessTokenVerifier{
		verifier: provider.Verifier(&gooidc.Config{
			SkipClientIDCheck: true,
		}),
		issuer:           iss,
		expectedAudience: expectedAudience,
	}, nil
}

// AccessTokenClaims is the slice of the Hydra-issued JWT we care about.
// Subject is the T-Bite user ID we set during the consent step.
type AccessTokenClaims struct {
	Subject  string    `json:"sub"`
	Expiry   time.Time `json:"-"`
	Scopes   []string  `json:"-"`
	Audience []string  `json:"-"`
	ClientID string    `json:"client_id"`

	// Extra claims forwarded from our consent step. These mirror what the
	// Authentik claim mapper produces so downstream code (e.g. tools_menu
	// resolvePlant fallback) can pick them up without an extra DB lookup
	// later if we ever want to skip the user lookup.
	Email      string `json:"email"`
	Name       string `json:"name"`
	TBiteRole  string `json:"tbite_role"`
	TBitePlant string `json:"tbite_plant"`
	TBiteDept  string `json:"tbite_department"`
}

// SubjectVerifier is the small surface idhttp.AuthMiddleware needs — a
// raw-token-to-subject mapping. AccessTokenVerifier satisfies it via the
// Verify method below; tests can substitute a stub without dragging in OIDC.
type SubjectVerifier struct {
	V *AccessTokenVerifier
}

// Verify implements idhttp.JWTVerifier. Returns the JWT subject claim or an
// error when the token is invalid/expired.
func (s SubjectVerifier) Verify(ctx context.Context, raw string) (string, error) {
	claims, err := s.V.Verify(ctx, raw)
	if err != nil {
		return "", err
	}
	return claims.Subject, nil
}

// Verify validates the JWT and returns its decoded claims, or an error when
// the token is unsigned by Hydra, expired, or otherwise malformed.
func (v *AccessTokenVerifier) Verify(ctx context.Context, raw string) (*AccessTokenClaims, error) {
	// go-oidc.Verify rejects tokens with `typ: at+jwt` (RFC 9068) by
	// default because it's the ID-token verifier; Hydra emits exactly
	// that. Strip the bearer prefix defensively in case callers passed
	// the full header value.
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))

	idt, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}

	// Pull extra claims; ignore decoder errors so unrelated fields don't
	// break otherwise-valid tokens.
	var raw_claims struct {
		Audience any    `json:"aud"`
		Scope    string `json:"scope"`
		Subject  string `json:"sub"`
		ClientID string `json:"client_id"`
		Ext      struct {
			Email      string `json:"email"`
			Name       string `json:"name"`
			TBiteRole  string `json:"tbite_role"`
			TBitePlant string `json:"tbite_plant"`
			TBiteDept  string `json:"tbite_department"`
		} `json:"ext"`
		// Hydra also surfaces top-level customs depending on session
		// shape; we accept both placement options.
		EmailTop      string `json:"email"`
		NameTop       string `json:"name"`
		TBiteRoleTop  string `json:"tbite_role"`
		TBitePlantTop string `json:"tbite_plant"`
		TBiteDeptTop  string `json:"tbite_department"`
	}
	_ = idt.Claims(&raw_claims)

	claims := &AccessTokenClaims{
		Subject:  pick(raw_claims.Subject, idt.Subject),
		Expiry:   idt.Expiry,
		Scopes:   strings.Fields(raw_claims.Scope),
		ClientID: raw_claims.ClientID,

		Email:      pick(raw_claims.Ext.Email, raw_claims.EmailTop),
		Name:       pick(raw_claims.Ext.Name, raw_claims.NameTop),
		TBiteRole:  pick(raw_claims.Ext.TBiteRole, raw_claims.TBiteRoleTop),
		TBitePlant: pick(raw_claims.Ext.TBitePlant, raw_claims.TBitePlantTop),
		TBiteDept:  pick(raw_claims.Ext.TBiteDept, raw_claims.TBiteDeptTop),
	}
	switch a := raw_claims.Audience.(type) {
	case string:
		claims.Audience = []string{a}
	case []any:
		for _, v := range a {
			if s, ok := v.(string); ok {
				claims.Audience = append(claims.Audience, s)
			}
		}
	}
	if claims.Subject == "" {
		return nil, errors.New("hydra token has no subject claim")
	}
	if !audienceAllowed(claims.Audience, v.expectedAudience) {
		return nil, errors.New("hydra token audience not accepted")
	}
	return claims, nil
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// audienceAllowed implements "validate the audience when present". A token
// with no aud is accepted (our single-tenant Hydra mints tokens without one,
// and DCR clients don't request our resource), but a token explicitly scoped
// to a different resource is rejected — closing the cross-resource token
// confusion path (RFC 9068) without breaking the common DCR case.
func audienceAllowed(tokenAud []string, expected string) bool {
	if expected == "" || len(tokenAud) == 0 {
		return true
	}
	for _, a := range tokenAud {
		if a == expected {
			return true
		}
	}
	return false
}
