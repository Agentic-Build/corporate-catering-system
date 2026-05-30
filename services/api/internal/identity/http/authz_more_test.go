package idhttp_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// statusOf extracts the HTTP status from a huma error.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	se, ok := err.(huma.StatusError)
	require.Truef(t, ok, "expected huma.StatusError, got %T", err)
	return se.GetStatus()
}

func ctxWith(u *identity.User) context.Context {
	return idhttp.ContextWithUser(context.Background(), u)
}

// === RequireRole / RequireEmployee / RequireAdmin ===

func TestRequireRole_Unauthenticated(t *testing.T) {
	_, err := idhttp.RequireRole(context.Background(), identity.RoleEmployee, "employee")
	assert.Equal(t, http.StatusUnauthorized, statusOf(t, err))
}

func TestRequireRole_WrongRole(t *testing.T) {
	u := &identity.User{ID: "u1", Role: identity.RoleWelfareAdmin}
	_, err := idhttp.RequireRole(ctxWith(u), identity.RoleEmployee, "employee")
	assert.Equal(t, http.StatusForbidden, statusOf(t, err))
	assert.Contains(t, err.Error(), "employee role required")
}

func TestRequireRole_Match(t *testing.T) {
	u := &identity.User{ID: "u1", Role: identity.RoleEmployee}
	got, err := idhttp.RequireRole(ctxWith(u), identity.RoleEmployee, "employee")
	require.NoError(t, err)
	assert.Same(t, u, got)
}

func TestRequireEmployee(t *testing.T) {
	emp := &identity.User{ID: "u1", Role: identity.RoleEmployee}
	got, err := idhttp.RequireEmployee(ctxWith(emp))
	require.NoError(t, err)
	assert.Same(t, emp, got)

	// wrong role → 403
	other := &identity.User{ID: "u2", Role: identity.RoleWelfareAdmin}
	_, err = idhttp.RequireEmployee(ctxWith(other))
	assert.Equal(t, http.StatusForbidden, statusOf(t, err))
}

func TestRequireAdmin(t *testing.T) {
	admin := &identity.User{ID: "a1", Role: identity.RoleWelfareAdmin}
	got, err := idhttp.RequireAdmin(ctxWith(admin))
	require.NoError(t, err)
	assert.Same(t, admin, got)

	// unauthenticated → 401
	_, err = idhttp.RequireAdmin(context.Background())
	assert.Equal(t, http.StatusUnauthorized, statusOf(t, err))
}

// === RequireVendor ===

func TestRequireVendor_Unauthenticated(t *testing.T) {
	_, _, err := idhttp.RequireVendor(context.Background())
	assert.Equal(t, http.StatusUnauthorized, statusOf(t, err))
}

func TestRequireVendor_WrongRole(t *testing.T) {
	u := &identity.User{ID: "u1", Role: identity.RoleEmployee}
	_, _, err := idhttp.RequireVendor(ctxWith(u))
	assert.Equal(t, http.StatusForbidden, statusOf(t, err))
	assert.Contains(t, err.Error(), "vendor operator required")
}

func TestRequireVendor_NilVendorID(t *testing.T) {
	u := &identity.User{ID: "u1", Role: identity.RoleVendorOperator, VendorID: nil}
	_, _, err := idhttp.RequireVendor(ctxWith(u))
	assert.Equal(t, http.StatusForbidden, statusOf(t, err))
	assert.Contains(t, err.Error(), "not bound to a vendor")
}

func TestRequireVendor_EmptyVendorID(t *testing.T) {
	empty := ""
	u := &identity.User{ID: "u1", Role: identity.RoleVendorOperator, VendorID: &empty}
	_, _, err := idhttp.RequireVendor(ctxWith(u))
	assert.Equal(t, http.StatusForbidden, statusOf(t, err))
	assert.Contains(t, err.Error(), "not bound to a vendor")
}

func TestRequireVendor_Success(t *testing.T) {
	vid := "vendor-42"
	u := &identity.User{ID: "u1", Role: identity.RoleVendorOperator, VendorID: &vid}
	got, gotVID, err := idhttp.RequireVendor(ctxWith(u))
	require.NoError(t, err)
	assert.Same(t, u, got)
	assert.Equal(t, "vendor-42", gotVID)
}

// === mapErr (exercised via MapErr export is not available; drive via handler) ===
// mapErr's remaining arms (Locked / 404 / default 500) are reached through the
// startLogin handler by making the service return the corresponding error.

// svcErrProvider lets Exchange / state lookups surface specific errors so
// startLogin -> mapErr hits each switch arm.

// errStates returns a chosen error from Get to drive mapErr through startLogin's
// sibling completeLogin path is awkward; instead we hit mapErr arms via a
// dedicated provider that fails StartLogin. StartLogin validates the provider
// first, so we use the states/provider plumbing already present.

// To cover mapErr's 423 (suspended), 404 (not found) and default(500) arms we
// drive completeLogin with a Service whose collaborators return those errors,
// and (for non-CallbackError errors) assert mapErr's mapping directly.

// mismatchExchangeProvider returns userinfo for a role; reused from fakeProvider.

// --- 423 Locked: ErrAccountSuspended without an AppURLs entry falls through to mapErr ---
func TestMapErr_AccountSuspendedLocked(t *testing.T) {
	suspended := &identity.User{ID: "u-sus", PrimaryEmail: "emp@tbite.test", Role: identity.RoleEmployee, Status: identity.StatusSuspended}
	svc := &identity.Service{
		Users:      &fakeUsers{byID: map[string]*identity.User{"u-sus": suspended}},
		Identities: linkedSuspendedIdentities{userID: "u-sus"},
		Sessions:   newFakeSessions(),
		Providers:  map[string]oidc.Provider{"authentik": fakeProvider{}},
		States: &fakeStates{m: map[string]*oidc.StatePayload{
			"st1": {App: "employee", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
		}},
	}
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{}, // no "employee" entry → CallbackError falls through to mapErr
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusLocked, resp.StatusCode)
}

// --- 400 BadRequest: oidc.ErrStateNotFound via unknown state ---
func TestMapErr_StateNotFoundBadRequest(t *testing.T) {
	svc := newSvc(&fakeStates{m: map[string]*oidc.StatePayload{}}) // empty → Get returns ErrStateNotFound
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{},
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// unknown state "ghost" → ErrStateNotFound. It is NOT a CallbackError, so it
	// goes straight to mapErr → 400.
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=ghost&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// --- default 500: a generic (non-sentinel) CallbackError with no AppURLs entry
// falls through to mapErr's default arm. We trigger a state/path provider
// mismatch, which CompleteLogin wraps as a plain fmt error inside CallbackError. ---
func TestMapErr_DefaultInternalServerError(t *testing.T) {
	// state says provider "other" but the request hits /auth/authentik/callback.
	svc := newSvc(&fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "merchant", Provider: "other", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}})
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{}, // no "merchant" entry → fall through to mapErr
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// --- callbackErrorCode default "auth_failed": same provider mismatch, but WITH
// an AppURLs entry so it redirects via /login?error=auth_failed. ---
func TestCallbackErrorCode_DefaultAuthFailed(t *testing.T) {
	svc := newSvc(&fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "merchant", Provider: "other", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}})
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{"merchant": "http://merchant.tbite.test"},
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "http://merchant.tbite.test/login?error=auth_failed", resp.Header.Get("Location"))
}

// --- 404 NotFound: an existing linked identity points at a user id absent from
// the Users repo, so CompleteLogin surfaces ErrUserNotFound (wrapped in
// CallbackError). With no AppURLs entry it falls through to mapErr → 404. ---
func TestMapErr_UserNotFound(t *testing.T) {
	svc := &identity.Service{
		Users:      &fakeUsers{byID: map[string]*identity.User{}}, // GetByID → ErrUserNotFound
		Identities: linkedSuspendedIdentities{userID: "ghost"},    // linked id resolves, user missing
		Sessions:   newFakeSessions(),
		Providers:  map[string]oidc.Provider{"authentik": fakeProvider{}},
		States: &fakeStates{m: map[string]*oidc.StatePayload{
			"st1": {App: "employee", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
		}},
	}
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{}, // no "employee" entry → fall through to mapErr
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
