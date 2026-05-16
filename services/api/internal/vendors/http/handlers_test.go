package vhttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	vhttp "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/http"
)

// Minimal stub session + user store, mirroring the pattern used in idhttp tests.

type stubSessions struct{ sess map[string]*identity.Session }

func (s *stubSessions) Create(_ context.Context, userID string, role identity.Role) (*identity.Session, error) {
	x := &identity.Session{Token: "tok", UserID: userID, Role: role, CreatedAt: time.Now(), LastSeenAt: time.Now()}
	s.sess[x.Token] = x
	return x, nil
}
func (s *stubSessions) Get(_ context.Context, t string) (*identity.Session, error) {
	if v, ok := s.sess[t]; ok {
		return v, nil
	}
	return nil, identity.ErrSessionNotFound
}
func (s *stubSessions) Touch(context.Context, string) error            { return nil }
func (s *stubSessions) Revoke(context.Context, string) error           { return nil }
func (s *stubSessions) RevokeAllForUser(context.Context, string) error { return nil }

type stubUsers struct{ byID map[string]*identity.User }

func (u *stubUsers) GetByID(_ context.Context, id string) (*identity.User, error) {
	if v, ok := u.byID[id]; ok {
		return v, nil
	}
	return nil, identity.ErrUserNotFound
}
func (u *stubUsers) GetByEmail(context.Context, string) (*identity.User, error) {
	return nil, identity.ErrUserNotFound
}
func (u *stubUsers) Create(context.Context, *identity.User) error                { return nil }
func (u *stubUsers) UpdateStatus(context.Context, string, identity.Status) error { return nil }
func (u *stubUsers) UpdateProfile(context.Context, *identity.User) error         { return nil }

// TestRequireAdmin_RejectsEmployee asserts admin-only vendor endpoints return
// 403 when called with an employee session.
func TestRequireAdmin_RejectsEmployee(t *testing.T) {
	user := &identity.User{
		ID: "u-emp", PrimaryEmail: "emp@x.com", DisplayName: "Emp",
		Role: identity.RoleEmployee, Status: identity.StatusActive,
	}
	sessions := &stubSessions{sess: map[string]*identity.Session{
		"tok-emp": {Token: "tok-emp", UserID: "u-emp", Role: identity.RoleEmployee},
	}}
	users := &stubUsers{byID: map[string]*identity.User{"u-emp": user}}

	idAPI := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	vendorAPI := &vhttp.API{Svc: nil} // Svc not reached when guard fails

	r := chi.NewRouter()
	r.Use(idAPI.AuthMiddleware)
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	vendorAPI.Register(h)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/admin/vendors", nil)
	req.Header.Set("Authorization", "Bearer tok-emp")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// TestRequireAdmin_RejectsAnonymous asserts the same endpoints return 401 when
// no Authorization header is present.
func TestRequireAdmin_RejectsAnonymous(t *testing.T) {
	sessions := &stubSessions{sess: map[string]*identity.Session{}}
	users := &stubUsers{byID: map[string]*identity.User{}}

	idAPI := &idhttp.API{Sessions: sessions, Users: users, AppURLs: map[string]string{}}
	vendorAPI := &vhttp.API{Svc: nil}

	r := chi.NewRouter()
	r.Use(idAPI.AuthMiddleware)
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	vendorAPI.Register(h)

	srv := httptest.NewServer(r)
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/admin/vendors", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
