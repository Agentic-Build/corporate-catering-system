package hydra

import (
	"errors"
	"testing"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

// paramFor's default branch is unreachable through the public AdminClient API
// (all paths contain login/consent); cover it white-box.
func TestParamFor(t *testing.T) {
	cases := map[string]string{
		"/admin/oauth2/auth/requests/login":   "login_challenge",
		"/admin/oauth2/auth/requests/consent": "consent_challenge",
		"/some/other/path":                    "challenge",
	}
	for path, want := range cases {
		if got := paramFor(path); got != want {
			t.Fatalf("paramFor(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"user string sentinel", errors.New("identity: user not found"), true},
		{"identity string sentinel", errors.New("identity: identity not found"), true},
		{"typed ErrUserNotFound", identity.ErrUserNotFound, true},
		{"typed ErrIdentityNotFound", identity.ErrIdentityNotFound, true},
		{"unrelated", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotFound(tc.err); got != tc.want {
				t.Fatalf("isNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestAudienceAllowed(t *testing.T) {
	const resource = "https://api.example.com/mcp"
	cases := []struct {
		name     string
		aud      []string
		expected string
		want     bool
	}{
		{"feature off accepts anything", []string{"https://other/api"}, "", true},
		{"no audience claim is accepted (single-tenant Hydra)", nil, resource, true},
		{"empty audience slice accepted", []string{}, resource, true},
		{"matching audience accepted", []string{resource}, resource, true},
		{"matching among many accepted", []string{"foo", resource}, resource, true},
		{"foreign audience rejected", []string{"https://other.example.com/api"}, resource, false},
		{"foreign audiences only rejected", []string{"a", "b"}, resource, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := audienceAllowed(tc.aud, tc.expected); got != tc.want {
				t.Fatalf("audienceAllowed(%v, %q) = %v, want %v", tc.aud, tc.expected, got, tc.want)
			}
		})
	}
}
