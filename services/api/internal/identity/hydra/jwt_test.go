package hydra

import "testing"

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
