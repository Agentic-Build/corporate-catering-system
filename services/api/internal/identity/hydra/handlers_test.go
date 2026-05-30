package hydra_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// === fakes (local to this package) ===

type fakeUserRepo struct {
	mu         sync.Mutex
	byID       map[string]*identity.User
	byEmail    map[string]*identity.User
	nextID     int
	createErr  error
	byEmailErr error
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: map[string]*identity.User{}, byEmail: map[string]*identity.User{}}
}

func (r *fakeUserRepo) GetByID(ctx context.Context, id string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byEmailErr != nil {
		return nil, r.byEmailErr
	}
	if u, ok := r.byEmail[email]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) Create(ctx context.Context, u *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createErr != nil {
		return r.createErr
	}
	r.nextID++
	u.ID = "uid-" + itoa(r.nextID)
	r.byID[u.ID] = u
	r.byEmail[u.PrimaryEmail] = u
	return nil
}

func (r *fakeUserRepo) UpdateProfile(ctx context.Context, u *identity.User) error { return nil }
func (r *fakeUserRepo) UpdateStatus(ctx context.Context, id string, s identity.Status) error {
	return nil
}

func (r *fakeUserRepo) put(u *identity.User) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[u.ID] = u
	r.byEmail[u.PrimaryEmail] = u
}

type fakeIdentityRepo struct {
	mu      sync.Mutex
	bySub   map[string]*identity.UserIdentity
	linkErr error
	getErr  error
}

func newFakeIdentityRepo() *fakeIdentityRepo {
	return &fakeIdentityRepo{bySub: map[string]*identity.UserIdentity{}}
}

func (r *fakeIdentityRepo) GetByProviderSubject(ctx context.Context, p identity.Provider, sub string) (*identity.UserIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	if x, ok := r.bySub[string(p)+":"+sub]; ok {
		return x, nil
	}
	return nil, identity.ErrIdentityNotFound
}

func (r *fakeIdentityRepo) Link(ctx context.Context, ui *identity.UserIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.linkErr != nil {
		return r.linkErr
	}
	r.bySub[string(ui.Provider)+":"+ui.ExternalSubject] = ui
	return nil
}

func (r *fakeIdentityRepo) ListByUser(ctx context.Context, userID string) ([]*identity.UserIdentity, error) {
	return nil, nil
}

type fakeStates struct {
	mu     sync.Mutex
	m      map[string]*oidc.StatePayload
	getErr error
}

func newFakeStates() *fakeStates { return &fakeStates{m: map[string]*oidc.StatePayload{}} }

func (s *fakeStates) Put(ctx context.Context, state string, p *oidc.StatePayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[state] = p
	return nil
}

func (s *fakeStates) Get(ctx context.Context, state string) (*oidc.StatePayload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if p, ok := s.m[state]; ok {
		return p, nil
	}
	return nil, oidc.ErrStateNotFound
}

func (s *fakeStates) Consume(ctx context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, state)
	return nil
}

// putErr makes Put fail, to exercise LoginHandler's state-store error path.
type errStates struct{ *fakeStates }

func (s errStates) Put(ctx context.Context, state string, p *oidc.StatePayload) error {
	return assertErr
}

var assertErr = &stubError{"state store boom"}

type stubError struct{ s string }

func (e *stubError) Error() string { return e.s }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// realProvider builds a *oidc.OIDCProvider backed by the fake issuer.
func realProvider(t *testing.T, iss *oidcIssuer) *oidc.OIDCProvider {
	t.Helper()
	p, err := oidc.New(context.Background(), oidc.Config{
		Slug:         "authentik",
		IssuerURL:    iss.URL(),
		ClientID:     "tbite-mcp",
		ClientSecret: "secret",
		RedirectURL:  "https://api.example.com/oauth/callback",
	})
	require.NoError(t, err)
	return p
}

// hydraAdmin builds an AdminClient backed by an httptest server that always
// accepts and returns a fixed redirect_to.
func hydraAdmin(t *testing.T) (*hydra.AdminClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/next","subject":"sub","requested_scope":["openid"]}`))
	}))
	return &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}, srv
}

// === LoginHandler ===

func TestLoginHandler_MissingChallenge(t *testing.T) {
	b := &hydra.Bridge{}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestLoginHandler_GetLoginRequestError(t *testing.T) {
	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{}}}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login?login_challenge=c", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestLoginHandler_Skip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET login request returns skip; PUT accept returns redirect.
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"skip":true,"subject":"u-skip"}`))
			return
		}
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/skip-next"}`))
	}))
	defer srv.Close()

	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login?login_challenge=c", nil))
	assert.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "https://hydra/skip-next", rr.Header().Get("Location"))
}

func TestLoginHandler_NoProviderConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skip":false}`))
	}))
	defer srv.Close()

	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login?login_challenge=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestLoginHandler_RedirectsToAuthentik(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skip":false}`))
	}))
	defer srv.Close()

	states := newFakeStates()
	b := &hydra.Bridge{
		Hydra:            &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()},
		OIDCProvider:     realProvider(t, iss),
		OIDCProviderName: "authentik",
		States:           states,
	}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login?login_challenge=chal", nil))
	require.Equal(t, http.StatusFound, rr.Code)
	loc := rr.Header().Get("Location")
	assert.Contains(t, loc, "/auth")
	// One state was stashed, carrying the login challenge in ReturnTo.
	states.mu.Lock()
	defer states.mu.Unlock()
	require.Len(t, states.m, 1)
	for _, p := range states.m {
		assert.Equal(t, "chal", p.ReturnTo)
		assert.Equal(t, "mcp", p.App)
	}
}

func TestLoginHandler_StateStoreError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"skip":false}`))
	}))
	defer srv.Close()

	b := &hydra.Bridge{
		Hydra:            &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()},
		OIDCProvider:     realProvider(t, iss),
		OIDCProviderName: "authentik",
		States:           errStates{newFakeStates()},
	}
	rr := httptest.NewRecorder()
	b.LoginHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/login?login_challenge=chal", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

// === CallbackHandler ===

// callbackBridge wires a Bridge whose OIDC exchange succeeds against iss,
// returning the user/identity fakes for assertions.
func callbackBridge(t *testing.T, iss *oidcIssuer) (*hydra.Bridge, *fakeUserRepo, *fakeIdentityRepo, *fakeStates, *httptest.Server) {
	admin, srv := hydraAdmin(t)
	users := newFakeUserRepo()
	ids := newFakeIdentityRepo()
	states := newFakeStates()
	b := &hydra.Bridge{
		Hydra:            admin,
		Users:            users,
		Identities:       ids,
		OIDCProvider:     realProvider(t, iss),
		OIDCProviderName: "authentik",
		States:           states,
	}
	return b, users, ids, states, srv
}

func TestCallbackHandler_MissingParams(t *testing.T) {
	b := &hydra.Bridge{}
	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallbackHandler_StateLookupError(t *testing.T) {
	b := &hydra.Bridge{States: newFakeStates()}
	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=missing&code=c", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestCallbackHandler_StateAppMismatch(t *testing.T) {
	states := newFakeStates()
	_ = states.Put(context.Background(), "s1", &oidc.StatePayload{App: "web"})
	b := &hydra.Bridge{States: states}
	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "state app mismatch")
}

func putMCPState(t *testing.T, states *fakeStates) {
	t.Helper()
	require.NoError(t, states.Put(context.Background(), "s1", &oidc.StatePayload{
		App: "mcp", Provider: "authentik", ReturnTo: "login-chal",
		PKCEVerifier: "v", Nonce: "n",
	}))
}

func TestCallbackHandler_ExchangeError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.tokenStatus = http.StatusBadRequest // token endpoint fails
	b, _, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "oidc exchange")
}

func TestCallbackHandler_MissingSubjectOrEmail(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	// Sub present but email empty → claims missing.
	iss.idTokenClaims = map[string]any{"sub": "ext-1", "nonce": "n"}
	b, _, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing subject/email")
}

func TestCallbackHandler_NewUserProvisioned(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-new", "email": "new@b.com", "name": "New User", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	require.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "https://hydra/next", rr.Header().Get("Location"))
	// User + identity created.
	u, err := users.GetByEmail(context.Background(), "new@b.com")
	require.NoError(t, err)
	assert.Equal(t, identity.RoleWelfareAdmin, u.Role)
	_, err = ids.GetByProviderSubject(context.Background(), "authentik", "ext-new")
	require.NoError(t, err)
}

func TestCallbackHandler_SuspendedUser(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-susp", "email": "s@b.com", "name": "S", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	// Pre-create a suspended linked user so resolve returns it.
	users.put(&identity.User{ID: "u-susp", PrimaryEmail: "s@b.com", Role: identity.RoleWelfareAdmin, Status: identity.StatusSuspended})

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusLocked, rr.Code)
	assert.Contains(t, rr.Body.String(), "account suspended")
}

func TestCallbackHandler_ResolveError_InvalidRole(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	// Unknown role → userFromOIDCClaims errors → resolve error → 500.
	iss.idTokenClaims = map[string]any{
		"sub": "ext-bad", "email": "bad@b.com", "name": "Bad", "nonce": "n",
		"tbite_role": "wizard",
	}
	b, _, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "user resolve")
}

func TestCallbackHandler_AcceptLoginError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-x", "email": "x@b.com", "name": "X", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	// Hydra admin that rejects the accept (PUT) with 500.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	users := newFakeUserRepo()
	ids := newFakeIdentityRepo()
	states := newFakeStates()
	b := &hydra.Bridge{
		Hydra:            &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()},
		Users:            users,
		Identities:       ids,
		OIDCProvider:     realProvider(t, iss),
		OIDCProviderName: "authentik",
		States:           states,
	}
	putMCPState(t, states)

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "accept login")
}

// === resolveOrProvisionUser via Callback: existing link & email-link paths ===

func TestCallbackHandler_ExistingLink(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-linked", "email": "linked@b.com", "name": "L", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	users.put(&identity.User{ID: "u-linked", PrimaryEmail: "linked@b.com", Role: identity.RoleWelfareAdmin, Status: identity.StatusActive})
	require.NoError(t, ids.Link(context.Background(), &identity.UserIdentity{
		UserID: "u-linked", Provider: "authentik", ExternalSubject: "ext-linked",
	}))

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusFound, rr.Code)
}

func TestCallbackHandler_EmailLink(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-fresh", "email": "exists@b.com", "name": "E", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	// Same email exists but NOT yet linked to this subject.
	users.put(&identity.User{ID: "u-exists", PrimaryEmail: "exists@b.com", Role: identity.RoleWelfareAdmin, Status: identity.StatusActive})

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	require.Equal(t, http.StatusFound, rr.Code)
	// New identity link created against the existing user.
	link, err := ids.GetByProviderSubject(context.Background(), "authentik", "ext-fresh")
	require.NoError(t, err)
	assert.Equal(t, "u-exists", link.UserID)
}

func TestCallbackHandler_EmailLink_LinkError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-fresh2", "email": "exists2@b.com", "name": "E", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	users.put(&identity.User{ID: "u-exists2", PrimaryEmail: "exists2@b.com", Role: identity.RoleWelfareAdmin, Status: identity.StatusActive})
	ids.linkErr = assertErr

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "link identity")
}

func TestCallbackHandler_NewUser_CreateError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-cerr", "email": "cerr@b.com", "name": "C", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	users.createErr = assertErr

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "create user")
}

func TestCallbackHandler_NewUser_LinkError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-lerr", "email": "lerr@b.com", "name": "C", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, _, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	ids.linkErr = assertErr

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "link identity")
}

func TestCallbackHandler_IdentityLookupError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-q", "email": "q@b.com", "name": "Q", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, _, ids, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	ids.getErr = assertErr // non-NotFound error short-circuits resolve

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "user resolve")
}

func TestCallbackHandler_EmailLookupError(t *testing.T) {
	iss := newOIDCIssuer(t, "tbite-mcp")
	iss.idTokenClaims = map[string]any{
		"sub": "ext-r", "email": "r@b.com", "name": "R", "nonce": "n",
		"tbite_role": "welfare_admin",
	}
	b, users, _, states, srv := callbackBridge(t, iss)
	defer srv.Close()
	putMCPState(t, states)
	users.byEmailErr = assertErr // non-NotFound error from GetByEmail

	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "user resolve")
}

// === role-specific provisioning (userFromOIDCClaims) ===

func runCallbackProvision(t *testing.T, claims map[string]any) (*httptest.ResponseRecorder, *fakeUserRepo) {
	t.Helper()
	iss := newOIDCIssuer(t, "tbite-mcp")
	claims["nonce"] = "n"
	iss.idTokenClaims = claims
	b, users, _, states, srv := callbackBridge(t, iss)
	t.Cleanup(srv.Close)
	putMCPState(t, states)
	rr := httptest.NewRecorder()
	b.CallbackHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/callback?state=s1&code=c", nil))
	return rr, users
}

func TestCallback_EmployeeProvision_Full(t *testing.T) {
	rr, users := runCallbackProvision(t, map[string]any{
		"sub": "emp-1", "email": "emp@b.com", "name": "Emp",
		"tbite_role":        "employee",
		"tbite_employee_id": "E123",
		"tbite_plant":       "TPE",
		"tbite_department":  "Logistics",
	})
	require.Equal(t, http.StatusFound, rr.Code)
	u, err := users.GetByEmail(context.Background(), "emp@b.com")
	require.NoError(t, err)
	require.NotNil(t, u.EmployeeID)
	assert.Equal(t, "E123", *u.EmployeeID)
	require.NotNil(t, u.Plant)
	assert.Equal(t, "TPE", *u.Plant)
	require.NotNil(t, u.Department)
	assert.Equal(t, "Logistics", *u.Department)
}

func TestCallback_EmployeeProvision_NoDepartment(t *testing.T) {
	rr, users := runCallbackProvision(t, map[string]any{
		"sub": "emp-2", "email": "emp2@b.com", "name": "Emp2",
		"tbite_role":        "employee",
		"tbite_employee_id": "E2",
		"tbite_plant":       "HSC",
	})
	require.Equal(t, http.StatusFound, rr.Code)
	u, _ := users.GetByEmail(context.Background(), "emp2@b.com")
	assert.Nil(t, u.Department)
}

func TestCallback_EmployeeProvision_MissingPlant(t *testing.T) {
	rr, _ := runCallbackProvision(t, map[string]any{
		"sub": "emp-3", "email": "emp3@b.com", "name": "Emp3",
		"tbite_role":        "employee",
		"tbite_employee_id": "E3",
		// plant missing
	})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "tbite_employee_id and tbite_plant")
}

func TestCallback_VendorProvision_Full(t *testing.T) {
	rr, users := runCallbackProvision(t, map[string]any{
		"sub": "vnd-1", "email": "vnd@b.com", "name": "Vnd",
		"tbite_role":      "vendor_operator",
		"tbite_vendor_id": "V99",
	})
	require.Equal(t, http.StatusFound, rr.Code)
	u, _ := users.GetByEmail(context.Background(), "vnd@b.com")
	require.NotNil(t, u.VendorID)
	assert.Equal(t, "V99", *u.VendorID)
}

func TestCallback_VendorProvision_MissingVendorID(t *testing.T) {
	rr, _ := runCallbackProvision(t, map[string]any{
		"sub": "vnd-2", "email": "vnd2@b.com", "name": "Vnd2",
		"tbite_role": "vendor_operator",
	})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "requires tbite_vendor_id")
}

// claimString: non-string value is treated as empty. A numeric tbite_role
// yields "" → invalid role error.
func TestCallback_NonStringRoleClaim(t *testing.T) {
	rr, _ := runCallbackProvision(t, map[string]any{
		"sub": "ns-1", "email": "ns@b.com", "name": "NS",
		"tbite_role": 12345, // not a string
	})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid tbite_role")
}

// === ConsentHandler ===

func TestConsentHandler_MissingChallenge(t *testing.T) {
	b := &hydra.Bridge{}
	rr := httptest.NewRecorder()
	b.ConsentHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/consent", nil))
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestConsentHandler_GetConsentError(t *testing.T) {
	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{}}}
	rr := httptest.NewRecorder()
	b.ConsentHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/consent?consent_challenge=c", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
}

func TestConsentHandler_HappyPath_WithClaims(t *testing.T) {
	var acceptBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"subject":"u-consent","requested_scope":["openid","offline_access"]}`))
			return
		}
		acceptBody = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/consent-done"}`))
	}))
	defer srv.Close()

	users := newFakeUserRepo()
	plant := "TPE"
	dept := "Eng"
	users.put(&identity.User{
		ID: "u-consent", PrimaryEmail: "c@b.com", DisplayName: "C",
		Role: identity.RoleEmployee, Plant: &plant, Department: &dept,
	})
	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}, Users: users}

	rr := httptest.NewRecorder()
	b.ConsentHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/consent?consent_challenge=cc", nil))
	require.Equal(t, http.StatusFound, rr.Code)
	assert.Equal(t, "https://hydra/consent-done", rr.Header().Get("Location"))
	q, _ := url.ParseQuery(acceptBody)
	assert.Equal(t, "cc", q.Get("consent_challenge"))
}

func TestConsentHandler_UserLookupMiss_StillAccepts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"subject":"unknown","requested_scope":["openid"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/no-claims"}`))
	}))
	defer srv.Close()

	users := newFakeUserRepo() // no users → GetByID misses, claims stay empty
	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}, Users: users}
	rr := httptest.NewRecorder()
	b.ConsentHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/consent?consent_challenge=cc", nil))
	assert.Equal(t, http.StatusFound, rr.Code)
}

func TestConsentHandler_AcceptError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"subject":"u","requested_scope":["openid"]}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := &hydra.Bridge{Hydra: &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}, Users: newFakeUserRepo()}
	rr := httptest.NewRecorder()
	b.ConsentHandler(rr, httptest.NewRequest(http.MethodGet, "/oauth/consent?consent_challenge=cc", nil))
	assert.Equal(t, http.StatusBadGateway, rr.Code)
	assert.Contains(t, rr.Body.String(), "accept consent")
}
