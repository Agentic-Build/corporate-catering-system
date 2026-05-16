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
