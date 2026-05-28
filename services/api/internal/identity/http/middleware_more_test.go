package idhttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// === extra fakes for error-branch / JWT coverage ===

// errSessions returns a non-NotFound error from Get to drive the 500 branch.
type errSessions struct{ *fakeSessions }

func (errSessions) Get(context.Context, string) (*identity.Session, error) {
	return nil, errors.New("redis down")
}

// errUsers returns an error from GetByID to drive the user-lookup 500 branch.
type errUsers struct{ *fakeUsers }

func (errUsers) GetByID(context.Context, string) (*identity.User, error) {
	return nil, errors.New("db down")
}

// fakeJWT is a stub idhttp.JWTVerifier. verify==nil means "valid" returning
// subject; a non-nil err makes Verify fail.
type fakeJWT struct {
	subject string
	err     error
}

func (j fakeJWT) Verify(context.Context, string) (string, error) {
	return j.subject, j.err
}

// fakeHandoff is an in-memory AuthHandoffStore.
type fakeHandoff struct {
	issueErr error
	codes    map[string]string // code -> token
}

func newFakeHandoff() *fakeHandoff { return &fakeHandoff{codes: map[string]string{}} }

func (h *fakeHandoff) IssueCode(_ context.Context, token string) (string, error) {
	if h.issueErr != nil {
		return "", h.issueErr
	}
	code := "code_" + token
	h.codes[code] = token
	return code, nil
}
func (h *fakeHandoff) RedeemCode(_ context.Context, code string) (string, error) {
	if tok, ok := h.codes[code]; ok {
		delete(h.codes, code)
		return tok, nil
	}
	return "", identity.ErrHandoffNotFound
}

// errRevokeSessions errors on Revoke to drive the logout 500 branch while
// still resolving Get for the auth middleware.
type errRevokeSessions struct{ *fakeSessions }

func (errRevokeSessions) Revoke(context.Context, string) error { return errors.New("revoke down") }

// linkedSuspendedIdentities resolves an existing identity so CompleteLogin
// loads an existing (suspended) user and returns ErrAccountSuspended.
type linkedSuspendedIdentities struct{ userID string }

func (l linkedSuspendedIdentities) GetByProviderSubject(context.Context, identity.Provider, string) (*identity.UserIdentity, error) {
	return &identity.UserIdentity{UserID: l.userID}, nil
}
func (linkedSuspendedIdentities) Link(context.Context, *identity.UserIdentity) error { return nil }
func (linkedSuspendedIdentities) ListByUser(context.Context, string) ([]*identity.UserIdentity, error) {
	return nil, nil
}

func newServer(t *testing.T, api *idhttp.API) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(buildHandler(api))
	t.Cleanup(srv.Close)
	return srv
}

// === AuthMiddleware branches ===

func TestAuthMiddleware_NoAuthHeader(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	// no Authorization header at all → anonymous → /me 401
	resp, err := http.Get(srv.URL + "/me")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthMiddleware_EmptyBearer(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer ")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthMiddleware_SessionStoreError(t *testing.T) {
	api := &idhttp.API{Sessions: errSessions{newFakeSessions()}, Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer tb_x")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestAuthMiddleware_UserLookupError(t *testing.T) {
	sessions := newFakeSessions()
	sessions.sessions["tb_x"] = &identity.Session{Token: "tb_x", UserID: "u1", Role: identity.RoleEmployee}
	api := &idhttp.API{Sessions: sessions, Users: errUsers{&fakeUsers{byID: map[string]*identity.User{}}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer tb_x")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestAuthMiddleware_SuspendedUser(t *testing.T) {
	user := &identity.User{ID: "u1", PrimaryEmail: "a@x.com", Role: identity.RoleEmployee, Status: identity.StatusSuspended}
	sessions := newFakeSessions()
	sessions.sessions["tb_x"] = &identity.Session{Token: "tb_x", UserID: "u1", Role: identity.RoleEmployee}
	api := &idhttp.API{Sessions: sessions, Users: &fakeUsers{byID: map[string]*identity.User{"u1": user}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer tb_x")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusLocked, resp.StatusCode)
}

func TestAuthMiddleware_JWTSuccess(t *testing.T) {
	user := &identity.User{ID: "u-jwt", PrimaryEmail: "j@x.com", DisplayName: "J", Role: identity.RoleEmployee, Status: identity.StatusActive}
	api := &idhttp.API{
		Sessions: newFakeSessions(), // unknown token → ErrSessionNotFound → JWT path
		Users:    &fakeUsers{byID: map[string]*identity.User{"u-jwt": user}},
		AppURLs:  map[string]string{},
		JWT:      fakeJWT{subject: "u-jwt"},
	}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer eyJ.jwt")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAuthMiddleware_JWTVerifyFails(t *testing.T) {
	api := &idhttp.API{
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  map[string]string{},
		JWT:      fakeJWT{err: errors.New("bad sig")},
	}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer eyJ.bad")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// verify fails → anonymous → /me 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthMiddleware_JWTSubjectNotFound(t *testing.T) {
	api := &idhttp.API{
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}}, // GetByID → ErrUserNotFound
		AppURLs:  map[string]string{},
		JWT:      fakeJWT{subject: "ghost"},
	}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer eyJ.jwt")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// user not found → anonymous → /me 401
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuthMiddleware_JWTSuspendedUser(t *testing.T) {
	user := &identity.User{ID: "u-sus", Role: identity.RoleEmployee, Status: identity.StatusSuspended}
	api := &idhttp.API{
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{"u-sus": user}},
		AppURLs:  map[string]string{},
		JWT:      fakeJWT{subject: "u-sus"},
	}
	srv := newServer(t, api)

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer eyJ.jwt")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusLocked, resp.StatusCode)
}

func TestContextWithUser_RoundTrip(t *testing.T) {
	u := &identity.User{ID: "u9"}
	ctx := idhttp.ContextWithUser(context.Background(), u)
	got, ok := idhttp.UserFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, u, got)
}

// === handler branches: startLogin / providers / exchangeSession / logout / completeLogin handoff ===

func newSvc(states *fakeStates) *identity.Service {
	return &identity.Service{
		Users:      &fakeUsers{byID: map[string]*identity.User{}},
		Identities: fakeIdentities{},
		Sessions:   newFakeSessions(),
		Providers:  map[string]oidc.Provider{"authentik": fakeProvider{}},
		States:     states,
	}
}

func TestStartLogin_Success(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  map[string]string{},
	}
	srv := newServer(t, api)

	body := strings.NewReader(`{"app":"employee","return_to":"/menu"}`)
	resp, err := http.Post(srv.URL+"/auth/authentik/start", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestStartLogin_InvalidProvider(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  map[string]string{},
	}
	srv := newServer(t, api)

	body := strings.NewReader(`{"app":"employee"}`)
	// unknown provider → Svc returns ErrInvalidProvider → mapErr → 400
	resp, err := http.Post(srv.URL+"/auth/nope/start", "application/json", body)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestProviders_List(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  map[string]string{},
	}
	srv := newServer(t, api)

	resp, err := http.Get(srv.URL + "/auth/providers")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestExchangeSession_NotConfigured(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	resp, err := http.Post(srv.URL+"/auth/session", "application/json", strings.NewReader(`{"code":"x"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestExchangeSession_EmptyCode(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}, Handoff: newFakeHandoff()}
	srv := newServer(t, api)

	resp, err := http.Post(srv.URL+"/auth/session", "application/json", strings.NewReader(`{"code":""}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestExchangeSession_InvalidCode(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}, Handoff: newFakeHandoff()}
	srv := newServer(t, api)

	resp, err := http.Post(srv.URL+"/auth/session", "application/json", strings.NewReader(`{"code":"bogus"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestExchangeSession_Success(t *testing.T) {
	handoff := newFakeHandoff()
	handoff.codes["good"] = "tb_session"
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}, Handoff: handoff}
	srv := newServer(t, api)

	resp, err := http.Post(srv.URL+"/auth/session", "application/json", strings.NewReader(`{"code":"good"}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestLogout_Unauthenticated(t *testing.T) {
	api := &idhttp.API{Sessions: newFakeSessions(), Users: &fakeUsers{byID: map[string]*identity.User{}}, AppURLs: map[string]string{}}
	srv := newServer(t, api)

	// no bearer → handler sees no token in ctx → 401
	resp, err := http.Post(srv.URL+"/auth/logout", "application/json", strings.NewReader(""))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLogout_RevokeError(t *testing.T) {
	user := &identity.User{ID: "u1", Role: identity.RoleEmployee, Status: identity.StatusActive}
	sessions := newFakeSessions()
	sessions.sessions["tb_x"] = &identity.Session{Token: "tb_x", UserID: "u1", Role: identity.RoleEmployee}
	api := &idhttp.API{
		Sessions: errRevokeSessions{sessions},
		Users:    &fakeUsers{byID: map[string]*identity.User{"u1": user}},
		AppURLs:  map[string]string{},
	}
	srv := newServer(t, api)

	req, _ := http.NewRequest("POST", srv.URL+"/auth/logout", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer tb_x")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// completeLogin's Handoff.IssueCode failure → 500.
func TestCompleteLogin_HandoffIssueError(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "employee", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{"employee": "http://app.tbite.test"},
		Handoff:  &fakeHandoff{issueErr: errors.New("redis down")},
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// A suspended existing user produces ErrAccountSuspended; with an AppURL entry
// the callback redirects to /login?error=account_suspended (covers the
// account_suspended arm of callbackErrorCode).
func TestCompleteLogin_AccountSuspendedRedirect(t *testing.T) {
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
		AppURLs:  idhttp.AppBaseURLs{"employee": "http://app.tbite.test"},
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	assert.Equal(t, "http://app.tbite.test/login?error=account_suspended", resp.Header.Get("Location"))
}

// completeLogin via the Handoff path issues a single-use code and redirects to
// /auth/landing?code=... rather than embedding the token.
func TestCompleteLogin_HandoffPath(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "employee", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{"employee": "http://app.tbite.test"},
		Handoff:  newFakeHandoff(),
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.True(t, strings.HasPrefix(loc, "http://app.tbite.test/auth/landing?code="), "got %q", loc)
	assert.Contains(t, loc, "return_to=%2Fmenu")
}

// completeLogin success but AppURLs has no entry for the resolved app → 500.
func TestCompleteLogin_UnknownAppBaseURL(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "employee", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	api := &idhttp.API{
		Svc:      newSvc(states),
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{}, // no "employee" entry
	}
	srv := newServer(t, api)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// CallbackError whose App has no AppURLs entry falls through to mapErr; a role
// mismatch maps to 403.
func TestCompleteLogin_CallbackErrorNoAppURL(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "merchant", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	api := &idhttp.API{
		Svc:      newSvc(states), // fakeProvider returns employee → role_mismatch for merchant
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
	assert.Equal(t, http.StatusForbidden, resp.StatusCode) // ErrRoleMismatch → 403
}
