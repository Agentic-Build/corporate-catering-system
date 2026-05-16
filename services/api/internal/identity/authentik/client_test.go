package authentik

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

func TestUpsertVendorOperatorCreatesUserAndRecoveryLink(t *testing.T) {
	var created userWriteRequest
	var sawRecovery bool
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			requireQuery(t, r.URL.Query(), "name", "tbite:role:vendor_operator")
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "group-vendor", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/":
			switch r.Method {
			case http.MethodGet:
				requireQuery(t, r.URL.Query(), "email", "operator@vendor.test")
				writeJSON(t, w, listResponse[userResponse]{})
			case http.MethodPost:
				decodeJSON(t, r, &created)
				writeJSON(t, w, userResponse{PK: 42, UUID: "user-uuid"})
			default:
				t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
			}
		case "/api/v3/core/users/42/recovery/":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
			}
			sawRecovery = true
			writeJSON(t, w, recoveryResponse{Link: "http://authentik.test/recover/token"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	c := newTestClient(t, srv.URL)
	out, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email:       " Operator@Vendor.Test ",
		DisplayName: "Vendor Operator",
		VendorID:    "vendor-1",
		Active:      true,
	})
	if err != nil {
		t.Fatalf("UpsertVendorOperator() error = %v", err)
	}
	if out.Provider != "authentik" || out.ExternalSubject != "user-uuid" || out.SetupURL == "" {
		t.Fatalf("unexpected provisioned output: %#v", out)
	}
	if !sawRecovery {
		t.Fatal("expected recovery link request")
	}
	if created.Username != "operator@vendor.test" || created.Email != "operator@vendor.test" {
		t.Fatalf("email was not normalized in create request: %#v", created)
	}
	if created.Name != "Vendor Operator" || !created.IsActive {
		t.Fatalf("unexpected create request profile: %#v", created)
	}
	if len(created.Groups) != 1 || created.Groups[0] != "group-vendor" {
		t.Fatalf("unexpected create request groups: %#v", created.Groups)
	}
	if created.Attributes["tbite_role"] != string(identity.RoleVendorOperator) ||
		created.Attributes["tbite_vendor_id"] != "vendor-1" {
		t.Fatalf("unexpected create request attributes: %#v", created.Attributes)
	}
}

func TestSuspendVendorOperatorDisablesAuthentikUser(t *testing.T) {
	var patched userWriteRequest
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/users/":
			requireQuery(t, r.URL.Query(), "uuid", "user-uuid")
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{
				PK:         42,
				UUID:       "user-uuid",
				Groups:     []string{"group-vendor"},
				Attributes: map[string]any{"tbite_vendor_id": "vendor-1"},
			}}})
		case "/api/v3/core/users/42/":
			if r.Method != http.MethodPatch {
				t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
			}
			decodeJSON(t, r, &patched)
			writeJSON(t, w, userResponse{PK: 42, UUID: "user-uuid"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	c := newTestClient(t, srv.URL)
	if err := c.SuspendVendorOperator(context.Background(), "authentik", "user-uuid"); err != nil {
		t.Fatalf("SuspendVendorOperator() error = %v", err)
	}
	if patched.IsActive {
		t.Fatalf("expected user to be patched inactive: %#v", patched)
	}
	if len(patched.Groups) != 1 || patched.Groups[0] != "group-vendor" {
		t.Fatalf("unexpected patched groups: %#v", patched.Groups)
	}
}

func TestReinstateVendorOperatorRestoresGroupAndClaims(t *testing.T) {
	var patched userWriteRequest
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/users/":
			requireQuery(t, r.URL.Query(), "uuid", "user-uuid")
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{
				PK:         42,
				UUID:       "user-uuid",
				Groups:     []string{"other-group"},
				Attributes: map[string]any{},
			}}})
		case "/api/v3/core/groups/":
			requireQuery(t, r.URL.Query(), "name", "tbite:role:vendor_operator")
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "group-vendor", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/42/":
			if r.Method != http.MethodPatch {
				t.Fatalf("unexpected method %s %s", r.Method, r.URL.Path)
			}
			decodeJSON(t, r, &patched)
			writeJSON(t, w, userResponse{PK: 42, UUID: "user-uuid"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	c := newTestClient(t, srv.URL)
	if err := c.ReinstateVendorOperator(context.Background(), "authentik", "user-uuid", "vendor-1"); err != nil {
		t.Fatalf("ReinstateVendorOperator() error = %v", err)
	}
	if !patched.IsActive {
		t.Fatalf("expected user to be patched active: %#v", patched)
	}
	if len(patched.Groups) != 2 || patched.Groups[0] != "other-group" || patched.Groups[1] != "group-vendor" {
		t.Fatalf("unexpected patched groups: %#v", patched.Groups)
	}
	if patched.Attributes["tbite_role"] != string(identity.RoleVendorOperator) ||
		patched.Attributes["tbite_vendor_id"] != "vendor-1" {
		t.Fatalf("unexpected patched attributes: %#v", patched.Attributes)
	}
}

func authentikTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		handler(w, r)
	}))
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{
		BaseURL:             baseURL,
		APIToken:            "token",
		Provider:            "authentik",
		VendorOperatorGroup: "tbite:role:vendor_operator",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func decodeJSON(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Fatalf("decode request: %v", err)
	}
}

func requireQuery(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("query %s = %q, want %q", key, got, want)
	}
}
