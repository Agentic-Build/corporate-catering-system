package hydra_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
)

func TestNewAccessTokenVerifier_RequiresReachableURL(t *testing.T) {
	_, err := hydra.NewAccessTokenVerifier(context.Background(), "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reachable URL is required")
}

func TestNewAccessTokenVerifier_DiscoveryFailure(t *testing.T) {
	// Point at a server that doesn't serve a discovery doc.
	_, err := hydra.NewAccessTokenVerifier(context.Background(), "http://127.0.0.1:1/nope", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydra oidc discovery")
}

// newVerifier builds a real verifier against the fake issuer. publicIssuer is
// passed through so the InsecureIssuerURLContext branch is exercised when set.
func newVerifier(t *testing.T, iss *oidcIssuer, publicIssuer, expectedAud string) *hydra.AccessTokenVerifier {
	t.Helper()
	v, err := hydra.NewAccessTokenVerifier(context.Background(), iss.URL(), publicIssuer, expectedAud)
	require.NoError(t, err)
	return v
}

func TestAccessTokenVerifier_Verify_HappyPath_ExtClaims(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "")

	tok := iss.sign(map[string]any{
		"sub":       "user-123",
		"client_id": "mcp-client",
		"scope":     "openid offline_access read",
		"aud":       []any{"resource-a", "resource-b"},
		"ext": map[string]any{
			"email":            "a@b.com",
			"name":             "Alice",
			"tbite_role":       "employee",
			"tbite_plant":      "TPE",
			"tbite_department": "Eng",
		},
	})
	// Pass with a "Bearer " prefix to exercise the trim path.
	claims, err := v.Verify(context.Background(), "Bearer "+tok)
	require.NoError(t, err)
	assert.Equal(t, "user-123", claims.Subject)
	assert.Equal(t, "mcp-client", claims.ClientID)
	assert.Equal(t, []string{"openid", "offline_access", "read"}, claims.Scopes)
	assert.Equal(t, []string{"resource-a", "resource-b"}, claims.Audience)
	assert.Equal(t, "a@b.com", claims.Email)
	assert.Equal(t, "Alice", claims.Name)
	assert.Equal(t, "employee", claims.TBiteRole)
	assert.Equal(t, "TPE", claims.TBitePlant)
	assert.Equal(t, "Eng", claims.TBiteDept)
}

func TestAccessTokenVerifier_Verify_TopLevelClaims_StringAud(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "")

	tok := iss.sign(map[string]any{
		"sub":              "user-9",
		"aud":              "single-resource",
		"email":            "top@b.com",
		"name":             "Top",
		"tbite_role":       "welfare_admin",
		"tbite_plant":      "HSC",
		"tbite_department": "HR",
	})
	claims, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, []string{"single-resource"}, claims.Audience)
	assert.Equal(t, "top@b.com", claims.Email)
	assert.Equal(t, "Top", claims.Name)
	assert.Equal(t, "welfare_admin", claims.TBiteRole)
	assert.Equal(t, "HSC", claims.TBitePlant)
	assert.Equal(t, "HR", claims.TBiteDept)
}

func TestAccessTokenVerifier_Verify_InvalidToken(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "")

	_, err := v.Verify(context.Background(), "not-a-jwt")
	require.Error(t, err)
}

func TestAccessTokenVerifier_Verify_NoSubject(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "")

	// go-oidc tolerates an empty sub; our code must reject it.
	tok := iss.sign(map[string]any{"sub": ""})
	_, err := v.Verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no subject claim")
}

func TestAccessTokenVerifier_Verify_AudienceRejected(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "https://api.example.com/mcp")

	tok := iss.sign(map[string]any{
		"sub": "user-1",
		"aud": "https://some-other-resource/api",
	})
	_, err := v.Verify(context.Background(), tok)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audience not accepted")
}

func TestAccessTokenVerifier_Verify_AudienceAccepted(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	const aud = "https://api.example.com/mcp"
	v := newVerifier(t, iss, "", aud)

	tok := iss.sign(map[string]any{"sub": "user-1", "aud": aud})
	claims, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.Subject)
}

// SubjectVerifier wraps the verifier; cover both its success and error legs.
func TestSubjectVerifier_Verify(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	v := newVerifier(t, iss, "", "")
	sv := hydra.SubjectVerifier{V: v}

	tok := iss.sign(map[string]any{"sub": "subject-42"})
	sub, err := sv.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "subject-42", sub)

	_, err = sv.Verify(context.Background(), "garbage")
	require.Error(t, err)
}

// publicIssuer != "" exercises the InsecureIssuerURLContext branch. We sign
// tokens with iss = publicIssuer so verification passes against the override.
func TestAccessTokenVerifier_PublicIssuerOverride(t *testing.T) {
	iss := newOIDCIssuer(t, "any")
	const publicIss = "https://public.example.com/"
	v := newVerifier(t, iss, publicIss, "")

	tok := iss.sign(map[string]any{"sub": "user-1", "iss": publicIss})
	claims, err := v.Verify(context.Background(), tok)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.Subject)
}
