package hydra

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

// AccessTokenVerifier validates JWT access tokens issued by Hydra. Uses the
// go-oidc verifier (despite the ID-token name) since it does JWKS + standard
// claim validation. SkipClientIDCheck=true: access tokens go to many DCR clients.
type AccessTokenVerifier struct {
	verifier *gooidc.IDTokenVerifier
	issuer   string
	// expectedAudience is this resource server's identifier (the MCP URL).
	// When set, a token carrying an audience must include it; empty disables.
	expectedAudience string
}

// NewAccessTokenVerifier resolves Hydra's discovery doc at boot and caches the
// JWKS-backed verifier. reachableURL is what the Go process can hit;
// publicIssuer (optional) is what Hydra advertises as the iss claim when the
// API reverse-proxies Hydra under its own host — these can differ, and
// InsecureIssuerURLContext is go-oidc's mechanism for that.
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

	// Extra claims forwarded from our consent step (mirror the Authentik
	// claim mapper) so downstream can skip the user lookup.
	Email      string `json:"email"`
	Name       string `json:"name"`
	TBiteRole  string `json:"tbite_role"`
	TBitePlant string `json:"tbite_plant"`
	TBiteDept  string `json:"tbite_department"`
}

// SubjectVerifier is the raw-token-to-subject surface idhttp.AuthMiddleware
// needs. Tests can substitute a stub without dragging in OIDC.
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
	// Strip the bearer prefix in case callers passed the full header value.
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "Bearer "))

	idt, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, err
	}

	// Ignore decoder errors so unrelated fields don't break otherwise-valid tokens.
	var rawClaims struct {
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
		// Hydra also surfaces top-level customs; accept both placements.
		EmailTop      string `json:"email"`
		NameTop       string `json:"name"`
		TBiteRoleTop  string `json:"tbite_role"`
		TBitePlantTop string `json:"tbite_plant"`
		TBiteDeptTop  string `json:"tbite_department"`
	}
	_ = idt.Claims(&rawClaims)

	claims := &AccessTokenClaims{
		Subject:  pick(rawClaims.Subject, idt.Subject),
		Expiry:   idt.Expiry,
		Scopes:   strings.Fields(rawClaims.Scope),
		ClientID: rawClaims.ClientID,

		Email:      pick(rawClaims.Ext.Email, rawClaims.EmailTop),
		Name:       pick(rawClaims.Ext.Name, rawClaims.NameTop),
		TBiteRole:  pick(rawClaims.Ext.TBiteRole, rawClaims.TBiteRoleTop),
		TBitePlant: pick(rawClaims.Ext.TBitePlant, rawClaims.TBitePlantTop),
		TBiteDept:  pick(rawClaims.Ext.TBiteDept, rawClaims.TBiteDeptTop),
	}
	switch a := rawClaims.Audience.(type) {
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

// audienceAllowed: validate audience only when present. No aud → accept (our
// single-tenant Hydra mints without one); aud scoped to a different resource
// → reject, closing the RFC 9068 cross-resource token-confusion path.
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
