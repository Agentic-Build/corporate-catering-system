package idhttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

// Reuse mocks from identity package via inline fakes here (smaller than re-exporting).
// Tests focus on HTTP behavior, not deep flow logic (already covered in Task 7).

type fakeSessions struct{ sessions map[string]*identity.Session }

func newFakeSessions() *fakeSessions {
	return &fakeSessions{sessions: map[string]*identity.Session{}}
}
func (s *fakeSessions) Create(_ context.Context, userID string, role identity.Role) (*identity.Session, error) {
	sess := &identity.Session{Token: "tb_test", UserID: userID, Role: role, CreatedAt: time.Now(), LastSeenAt: time.Now()}
	s.sessions[sess.Token] = sess
	return sess, nil
}
func (s *fakeSessions) Get(_ context.Context, token string) (*identity.Session, error) {
	if x, ok := s.sessions[token]; ok {
		return x, nil
	}
	return nil, identity.ErrSessionNotFound
}
func (s *fakeSessions) Touch(context.Context, string) error { return nil }
func (s *fakeSessions) Revoke(_ context.Context, token string) error {
	delete(s.sessions, token)
	return nil
}
func (s *fakeSessions) RevokeAllForUser(context.Context, string) error { return nil }

type fakeUsers struct{ byID map[string]*identity.User }

func (u *fakeUsers) GetByID(_ context.Context, id string) (*identity.User, error) {
	if x, ok := u.byID[id]; ok {
		return x, nil
	}
	return nil, identity.ErrUserNotFound
}

// stubs for interface satisfaction
func (u *fakeUsers) GetByEmail(context.Context, string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}
func (u *fakeUsers) Create(context.Context, *identity.User) error                { return nil }
func (u *fakeUsers) UpdateStatus(context.Context, string, identity.Status) error { return nil }
func (u *fakeUsers) UpdateProfile(context.Context, *identity.User) error         { return nil }

func buildHandler(api *idhttp.API) http.Handler {
	r := chi.NewRouter()
	r.Use(api.AuthMiddleware)
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)
	return r
}

func TestMe_Unauthenticated(t *testing.T) {
	sessions := newFakeSessions()
	users := &fakeUsers{byID: map[string]*identity.User{}}
	api := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 401, resp.StatusCode)
}

func TestMe_Authenticated(t *testing.T) {
	user := &identity.User{ID: "u1", PrimaryEmail: "a@x.com", DisplayName: "Alice", Role: identity.RoleEmployee, Status: identity.StatusActive}
	sessions := newFakeSessions()
	sessions.sessions["tb_test"] = &identity.Session{Token: "tb_test", UserID: "u1", Role: identity.RoleEmployee}
	users := &fakeUsers{byID: map[string]*identity.User{"u1": user}}
	api := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer tb_test")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "a@x.com", body["email"])
	assert.Equal(t, "employee", body["role"])
}

func TestLogout_Authenticated(t *testing.T) {
	user := &identity.User{ID: "u2", PrimaryEmail: "b@x.com", DisplayName: "B", Role: identity.RoleEmployee, Status: identity.StatusActive}
	sessions := newFakeSessions()
	sessions.sessions["tb_logout"] = &identity.Session{Token: "tb_logout", UserID: "u2", Role: identity.RoleEmployee}
	users := &fakeUsers{byID: map[string]*identity.User{"u2": user}}
	api := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/auth/logout", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer tb_logout")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 204, resp.StatusCode)
	_, exists := sessions.sessions["tb_logout"]
	assert.False(t, exists, "session should be deleted")
}

func TestAuthMiddleware_IgnoresInvalidToken(t *testing.T) {
	sessions := newFakeSessions()
	users := &fakeUsers{byID: map[string]*identity.User{}}
	api := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/me", nil)
	req.Header.Set("Authorization", "Bearer tb_invalid")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	// invalid bearer → treated as anonymous → 401 from /me
	assert.Equal(t, 401, resp.StatusCode)
}

// ----- completeLogin redirect target (B4: mobile OIDC deep link) -----

// fakeIdentities satisfies identity.UserIdentityRepository; the callback flow
// always finds no existing identity and links a fresh one.
type fakeIdentities struct{}

func (fakeIdentities) GetByProviderSubject(context.Context, identity.Provider, string) (*identity.UserIdentity, error) {
	return nil, identity.ErrIdentityNotFound
}
func (fakeIdentities) Link(context.Context, *identity.UserIdentity) error { return nil }
func (fakeIdentities) ListByUser(context.Context, string) ([]*identity.UserIdentity, error) {
	return nil, nil
}

// fakeStates is an in-memory oidc.StateStore.
type fakeStates struct{ m map[string]*oidc.StatePayload }

func (s *fakeStates) Put(_ context.Context, state string, p *oidc.StatePayload) error {
	s.m[state] = p
	return nil
}
func (s *fakeStates) Get(_ context.Context, state string) (*oidc.StatePayload, error) {
	if p, ok := s.m[state]; ok {
		return p, nil
	}
	return nil, oidc.ErrStateNotFound
}
func (s *fakeStates) Consume(_ context.Context, state string) error {
	delete(s.m, state)
	return nil
}

// fakeProvider returns a fixed employee Userinfo on Exchange.
type fakeProvider struct{}

func (fakeProvider) Name() string        { return "authentik" }
func (fakeProvider) DisplayName() string { return "Authentik" }
func (fakeProvider) BuildAuthURL(_ context.Context, state string) (*oidc.AuthURL, error) {
	return &oidc.AuthURL{URL: "https://fake/" + state, PKCEVerifier: "v", Nonce: "n"}, nil
}
func (fakeProvider) Exchange(context.Context, string, string, string) (*oidc.Userinfo, error) {
	return &oidc.Userinfo{
		Provider: "authentik", ExternalSubject: "ak-1",
		Email: "emp@tbite.test", EmailVerified: true, DisplayName: "Emp",
		Raw: map[string]any{
			"tbite_role":        "employee",
			"tbite_employee_id": "E001",
			"tbite_plant":       "F12B-3F",
		},
	}, nil
}

// completeLoginRedirect drives a GET /auth/authentik/callback and returns the
// Location header along with the API instance. The OIDC state is pre-seeded
// with the given app value.
func completeLoginRedirect(t *testing.T, app string) (string, *idhttp.API) {
	t.Helper()
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: app, Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	svc := &identity.Service{
		Users:      &fakeUsers{byID: map[string]*identity.User{}},
		Identities: fakeIdentities{},
		Sessions:   newFakeSessions(),
		Providers:  map[string]oidc.Provider{"authentik": fakeProvider{}},
		States:     states,
	}
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{"employee": "http://app.tbite.test"},
	}
	srv := httptest.NewServer(buildHandler(api))
	t.Cleanup(srv.Close)

	// Don't follow the redirect; inspect the Location header directly.
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusFound, resp.StatusCode)
	return resp.Header.Get("Location"), api
}

func TestCompleteLogin_WebAppRedirectsToLanding(t *testing.T) {
	loc, _ := completeLoginRedirect(t, "employee")
	assert.True(t, strings.HasPrefix(loc, "http://app.tbite.test/auth/landing?token="),
		"expected web landing URL, got %q", loc)
	assert.Contains(t, loc, "return_to=%2Fmenu")
}

// An employee logging into the merchant app must be redirected back to the
// merchant /login with an error code — never shown raw JSON (issue X2).
func TestCompleteLogin_RoleMismatchRedirectsToAppLogin(t *testing.T) {
	states := &fakeStates{m: map[string]*oidc.StatePayload{
		"st1": {App: "merchant", Provider: "authentik", ReturnTo: "/menu", PKCEVerifier: "v", Nonce: "n"},
	}}
	svc := &identity.Service{
		Users:      &fakeUsers{byID: map[string]*identity.User{}},
		Identities: fakeIdentities{},
		Sessions:   newFakeSessions(),
		Providers:  map[string]oidc.Provider{"authentik": fakeProvider{}},
		States:     states,
	}
	api := &idhttp.API{
		Svc:      svc,
		Sessions: newFakeSessions(),
		Users:    &fakeUsers{byID: map[string]*identity.User{}},
		AppURLs:  idhttp.AppBaseURLs{"merchant": "http://merchant.tbite.test"},
	}
	srv := httptest.NewServer(buildHandler(api))
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, _ := http.NewRequest("GET", srv.URL+"/auth/authentik/callback?state=st1&code=C", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc := resp.Header.Get("Location")
	assert.Equal(t, "http://merchant.tbite.test/login?error=role_mismatch", loc)
}
