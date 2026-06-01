package authentik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

// --- New() validation paths ---

func TestNewValidation(t *testing.T) {
	valid := Config{
		BaseURL:             "http://authentik.test",
		APIToken:            "token",
		Provider:            "authentik",
		VendorOperatorGroup: "tbite:role:vendor_operator",
	}

	t.Run("missing base url", func(t *testing.T) {
		cfg := valid
		cfg.BaseURL = "  "
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for missing base url")
		}
	})

	t.Run("invalid base url", func(t *testing.T) {
		cfg := valid
		cfg.BaseURL = "://nope"
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for invalid base url")
		}
	})

	t.Run("base url without scheme", func(t *testing.T) {
		cfg := valid
		cfg.BaseURL = "authentik.test"
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for base url without scheme/host")
		}
	})

	t.Run("missing api token", func(t *testing.T) {
		cfg := valid
		cfg.APIToken = ""
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for missing api token")
		}
	})

	t.Run("missing provider", func(t *testing.T) {
		cfg := valid
		cfg.Provider = ""
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for missing provider")
		}
	})

	t.Run("missing vendor operator group", func(t *testing.T) {
		cfg := valid
		cfg.VendorOperatorGroup = ""
		if _, err := New(cfg); err == nil {
			t.Fatal("expected error for missing vendor operator group")
		}
	})

	t.Run("defaults http client when nil", func(t *testing.T) {
		cfg := valid
		cfg.HTTPClient = nil
		c, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if c.httpClient == nil {
			t.Fatal("expected default http client")
		}
	})

	t.Run("trims trailing slash", func(t *testing.T) {
		cfg := valid
		cfg.BaseURL = "http://authentik.test/"
		c, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if c.baseURL.String() != "http://authentik.test" {
			t.Fatalf("unexpected baseURL %q", c.baseURL.String())
		}
	})
}

// --- UpsertVendorOperator validation & error/edge paths ---

func TestUpsertVendorOperatorRequiredFields(t *testing.T) {
	c := newTestClient(t, "http://unused.test")
	cases := []identity.VendorOperatorProvisionInput{
		{Email: "", DisplayName: "Name", VendorID: "v1"},
		{Email: "a@b.test", DisplayName: " ", VendorID: "v1"},
		{Email: "a@b.test", DisplayName: "Name", VendorID: ""},
	}
	for _, in := range cases {
		if _, err := c.UpsertVendorOperator(context.Background(), in); err == nil {
			t.Fatalf("expected error for input %#v", in)
		}
	}
}

func TestUpsertVendorOperatorGroupLookupError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/core/groups/" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1", Active: true,
	})
	if err == nil {
		t.Fatal("expected group lookup error")
	}
}

func TestUpsertVendorOperatorUserLookupError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/":
			w.WriteHeader(http.StatusBadGateway)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected user lookup error")
	}
}

// Existing user path: patch instead of create, reuse attrs/groups.
func TestUpsertVendorOperatorExistingUserPatches(t *testing.T) {
	var patched userWriteRequest
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "group-vendor", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/":
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{
				PK:         7,
				UUID:       "existing-uuid",
				Groups:     []string{"existing-group"},
				Attributes: map[string]any{"keep": "me"},
			}}})
		case "/api/v3/core/users/7/":
			if r.Method != http.MethodPatch {
				t.Fatalf("unexpected method %s", r.Method)
			}
			decodeJSON(t, r, &patched)
			writeJSON(t, w, userResponse{PK: 7, UUID: "existing-uuid"})
		case "/api/v3/core/users/7/recovery/":
			writeJSON(t, w, recoveryResponse{Link: "http://authentik.test/recover/x"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	out, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v2", Active: true,
	})
	if err != nil {
		t.Fatalf("UpsertVendorOperator() error = %v", err)
	}
	if out.ExternalSubject != "existing-uuid" {
		t.Fatalf("unexpected subject %q", out.ExternalSubject)
	}
	if patched.Attributes["keep"] != "me" {
		t.Fatalf("expected existing attributes preserved: %#v", patched.Attributes)
	}
	if len(patched.Groups) != 2 || patched.Groups[1] != "group-vendor" {
		t.Fatalf("unexpected patched groups: %#v", patched.Groups)
	}
}

// Regression: an existing Authentik user whose `attributes` is null/omitted
// unmarshals to a nil map. UpsertVendorOperator must not write into that nil
// map (which panics with "assignment to entry in nil map") — it should start
// from a fresh map, mirroring the guard already present in the operator-patch
// path. Before the fix this test panicked and crashed the package.
func TestUpsertVendorOperatorExistingUserNilAttributes(t *testing.T) {
	var patched userWriteRequest
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "group-vendor", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/":
			// No Attributes field -> nil map after JSON decode.
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{
				PK:     7,
				UUID:   "existing-uuid",
				Groups: []string{"existing-group"},
			}}})
		case "/api/v3/core/users/7/":
			decodeJSON(t, r, &patched)
			writeJSON(t, w, userResponse{PK: 7, UUID: "existing-uuid"})
		case "/api/v3/core/users/7/recovery/":
			writeJSON(t, w, recoveryResponse{Link: "http://authentik.test/recover/x"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	out, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v2", Active: true,
	})
	if err != nil {
		t.Fatalf("UpsertVendorOperator() error = %v", err)
	}
	if out.ExternalSubject != "existing-uuid" {
		t.Fatalf("unexpected subject %q", out.ExternalSubject)
	}
	if patched.Attributes["tbite_role"] != string(identity.RoleVendorOperator) {
		t.Fatalf("expected tbite_role written into fresh attrs map: %#v", patched.Attributes)
	}
	if patched.Attributes["tbite_vendor_id"] != "v2" {
		t.Fatalf("expected tbite_vendor_id set: %#v", patched.Attributes)
	}
}

// Create user fails -> error propagated.
func TestUpsertVendorOperatorCreateError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodGet:
			writeJSON(t, w, listResponse[userResponse]{})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected create error")
	}
}

// Created user missing UUID -> explicit error.
func TestUpsertVendorOperatorMissingUUID(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodGet:
			writeJSON(t, w, listResponse[userResponse]{})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodPost:
			writeJSON(t, w, userResponse{PK: 1, UUID: ""})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected missing uuid error")
	}
}

// Recovery link fails -> error propagated.
func TestUpsertVendorOperatorRecoveryError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodGet:
			writeJSON(t, w, listResponse[userResponse]{})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodPost:
			writeJSON(t, w, userResponse{PK: 5, UUID: "uuid"})
		case r.URL.Path == "/api/v3/core/users/5/recovery/":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected recovery error")
	}
}

// recoveryLink with empty link -> error.
func TestUpsertVendorOperatorRecoveryMissingLink(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodGet:
			writeJSON(t, w, listResponse[userResponse]{})
		case r.URL.Path == "/api/v3/core/users/" && r.Method == http.MethodPost:
			writeJSON(t, w, userResponse{PK: 5, UUID: "uuid"})
		case r.URL.Path == "/api/v3/core/users/5/recovery/":
			writeJSON(t, w, recoveryResponse{Link: ""})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected missing recovery link error")
	}
}

// --- SuspendVendorOperator paths ---

func TestSuspendVendorOperatorUnsupportedProvider(t *testing.T) {
	c := newTestClient(t, "http://unused.test")
	if err := c.SuspendVendorOperator(context.Background(), "other", "uuid"); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestSuspendVendorOperatorLookupError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newTestClient(t, srv.URL)
	if err := c.SuspendVendorOperator(context.Background(), "authentik", "uuid"); err == nil {
		t.Fatal("expected lookup error")
	}
}

func TestSuspendVendorOperatorUserNotFound(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, listResponse[userResponse]{})
	})
	c := newTestClient(t, srv.URL)
	if err := c.SuspendVendorOperator(context.Background(), "authentik", "uuid"); err == nil {
		t.Fatal("expected user not found error")
	}
}

// --- ReinstateVendorOperator paths ---

func TestReinstateVendorOperatorUnsupportedProvider(t *testing.T) {
	c := newTestClient(t, "http://unused.test")
	if err := c.ReinstateVendorOperator(context.Background(), "other", "uuid", "v1"); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestReinstateVendorOperatorLookupError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newTestClient(t, srv.URL)
	if err := c.ReinstateVendorOperator(context.Background(), "authentik", "uuid", "v1"); err == nil {
		t.Fatal("expected lookup error")
	}
}

func TestReinstateVendorOperatorUserNotFound(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, listResponse[userResponse]{})
	})
	c := newTestClient(t, srv.URL)
	if err := c.ReinstateVendorOperator(context.Background(), "authentik", "uuid", "v1"); err == nil {
		t.Fatal("expected user not found error")
	}
}

func TestReinstateVendorOperatorGroupError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/users/":
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{PK: 1, UUID: "uuid"}}})
		case "/api/v3/core/groups/":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	if err := c.ReinstateVendorOperator(context.Background(), "authentik", "uuid", "v1"); err == nil {
		t.Fatal("expected group lookup error")
	}
}

// nil attributes on existing user -> reinstate initializes attrs map.
func TestReinstateVendorOperatorNilAttributes(t *testing.T) {
	var patched userWriteRequest
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/users/":
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{PK: 1, UUID: "uuid", Attributes: nil}}})
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/1/":
			decodeJSON(t, r, &patched)
			writeJSON(t, w, userResponse{PK: 1, UUID: "uuid"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	if err := c.ReinstateVendorOperator(context.Background(), "authentik", "uuid", "v9"); err != nil {
		t.Fatalf("ReinstateVendorOperator() error = %v", err)
	}
	if patched.Attributes["tbite_vendor_id"] != "v9" {
		t.Fatalf("unexpected attributes: %#v", patched.Attributes)
	}
}

// Existing user patch fails -> error propagated (covers patchUser error branch).
func TestUpsertVendorOperatorPatchError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "tbite:role:vendor_operator"}}})
		case "/api/v3/core/users/":
			writeJSON(t, w, listResponse[userResponse]{Results: []userResponse{{PK: 3, UUID: "uuid", Attributes: map[string]any{}}}})
		case "/api/v3/core/users/3/":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected patch error")
	}
}

// --- groupByName: results present but none matching name ---

func TestGroupByNameNoMatch(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{Results: []groupResponse{{PK: "g", Name: "different-name"}}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected group not found error when no result matches name")
	}
}

func TestGroupByNameEmptyResults(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/core/groups/":
			writeJSON(t, w, listResponse[groupResponse]{})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})
	c := newTestClient(t, srv.URL)
	_, err := c.UpsertVendorOperator(context.Background(), identity.VendorOperatorProvisionInput{
		Email: "op@vendor.test", DisplayName: "Op", VendorID: "v1",
	})
	if err == nil {
		t.Fatal("expected group not found error for empty results")
	}
}

// --- do(): transport error, marshal error, decode error, 204 no content ---

func TestDoTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // ensure connection refused
	c := newTestClient(t, url)
	// userByUUID -> do GET; transport fails -> error from SuspendVendorOperator lookup.
	if err := c.SuspendVendorOperator(context.Background(), "authentik", "uuid"); err == nil {
		t.Fatal("expected transport error")
	}
}

func TestDoMarshalError(t *testing.T) {
	// Body that cannot be marshalled (channel) reaches do via createUser? Not reachable
	// through public API, so exercise do directly with an unmarshalable body.
	c := newTestClient(t, "http://authentik.test")
	err := c.do(context.Background(), http.MethodPost, "/api/v3/core/users/", make(chan int), nil)
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestDoNewRequestError(t *testing.T) {
	c := newTestClient(t, "http://authentik.test")
	// Invalid method triggers http.NewRequestWithContext error.
	err := c.do(context.Background(), "bad method", "/api/v3/core/users/", nil, nil)
	if err == nil {
		t.Fatal("expected new request error")
	}
}

func TestDoDecodeError(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})
	c := newTestClient(t, srv.URL)
	var out userResponse
	err := c.do(context.Background(), http.MethodGet, "/api/v3/core/users/", nil, &out)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestDoNoContent(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	c := newTestClient(t, srv.URL)
	var out userResponse
	if err := c.do(context.Background(), http.MethodGet, "/api/v3/core/users/", nil, &out); err != nil {
		t.Fatalf("do() error = %v", err)
	}
}

func TestDoNilOut(t *testing.T) {
	srv := authentikTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]string{"ok": "1"})
	})
	c := newTestClient(t, srv.URL)
	if err := c.do(context.Background(), http.MethodGet, "/api/v3/core/users/", nil, nil); err != nil {
		t.Fatalf("do() error = %v", err)
	}
}

// --- appendMissing: already present returns unchanged slice ---

func TestAppendMissingAlreadyPresent(t *testing.T) {
	in := []string{"a", "b"}
	out := appendMissing(in, "a")
	if len(out) != 2 {
		t.Fatalf("expected unchanged slice, got %#v", out)
	}
}
