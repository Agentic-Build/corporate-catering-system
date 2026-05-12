package identity_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

func buildSvc() (*identity.Service, *fakeUserRepo, *fakeIdentityRepo, *fakeDir, *fakeAdminWL, *fakeSessions, *fakeStates, *fakeProvider, *fakeProvider, *fakeInvites) {
	users := newFakeUserRepo()
	ids := newFakeIdentityRepo()
	dir := &fakeDir{byEmail: map[string]*identity.EmployeeDirectoryEntry{}}
	aw := &fakeAdminWL{emails: map[string]struct{}{}}
	sess := newFakeSessions()
	st := newFakeStates()
	inv := newFakeInvites()
	g := &fakeProvider{name: "google"}
	gh := &fakeProvider{name: "github"}
	svc := &identity.Service{
		Users: users, Identities: ids, Directory: dir, Invites: inv, AdminWL: aw,
		Sessions: sess, Providers: map[string]oidc.Provider{"google": g, "github": gh},
		States: st, Clock: fixedClock{t: time.Now().UTC()},
	}
	return svc, users, ids, dir, aw, sess, st, g, gh, inv
}

func TestService_StartLogin_HappyEmployeeGoogle(t *testing.T) {
	svc, _, _, _, _, _, _, _, _, _ := buildSvc()
	out, err := svc.StartLogin(context.Background(), identity.StartLoginInput{
		App: "employee", Provider: "google", ReturnTo: "/menu",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.AuthURL)
	assert.NotEmpty(t, out.State)
}

func TestService_StartLogin_InvalidApp(t *testing.T) {
	svc, _, _, _, _, _, _, _, _, _ := buildSvc()
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "x", Provider: "google"})
	assert.Error(t, err)
}

func TestService_StartLogin_InvalidProvider(t *testing.T) {
	svc, _, _, _, _, _, _, _, _, _ := buildSvc()
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "employee", Provider: "facebook"})
	assert.ErrorIs(t, err, identity.ErrInvalidProvider)
}

func TestService_CompleteLogin_EmployeeHappy(t *testing.T) {
	svc, _, _, dir, _, _, st, g, _, _ := buildSvc()
	dir.byEmail["alice@tsmc.com"] = &identity.EmployeeDirectoryEntry{
		EmployeeID: "E001", PrimaryEmail: "alice@tsmc.com",
		DisplayName: "Alice", Status: identity.StatusActive,
	}
	st.m["S1"] = &oidc.StatePayload{App: "employee", Provider: "google", PKCEVerifier: "v", Nonce: "n", ReturnTo: "/"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-001", Email: "alice@tsmc.com", EmailVerified: true, DisplayName: "Alice"}
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "employee", State: "S1", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleEmployee, out.User.Role)
	assert.Equal(t, "E001", *out.User.EmployeeID)
	assert.NotEmpty(t, out.Session.Token)
}

func TestService_CompleteLogin_EmployeeNotInDirectory(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, _ := buildSvc()
	st.m["S2"] = &oidc.StatePayload{App: "employee", Provider: "google", PKCEVerifier: "v", Nonce: "n"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-002", Email: "stranger@x.com", EmailVerified: true, DisplayName: "X"}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "employee", State: "S2", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrNotInDirectory)
}

func TestService_CompleteLogin_AdminHappy(t *testing.T) {
	svc, _, _, _, aw, _, st, g, _, _ := buildSvc()
	aw.emails["root@tbite.com"] = struct{}{}
	st.m["S3"] = &oidc.StatePayload{App: "admin", Provider: "google", PKCEVerifier: "v", Nonce: "n"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-003", Email: "root@tbite.com", EmailVerified: true, DisplayName: "Root"}
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "admin", State: "S3", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleWelfareAdmin, out.User.Role)
}

func TestService_CompleteLogin_AdminNotInWhitelist(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, _ := buildSvc()
	st.m["S4"] = &oidc.StatePayload{App: "admin", Provider: "google", PKCEVerifier: "v", Nonce: "n"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-004", Email: "x@x.com", EmailVerified: true}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "admin", State: "S4", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrNotInAdminWhitelist)
}

func TestService_CompleteLogin_Suspended(t *testing.T) {
	svc, users, _, dir, _, _, st, g, _, _ := buildSvc()
	// pre-seed a suspended user
	suspended := &identity.User{PrimaryEmail: "sus@tsmc.com", DisplayName: "Sus", Role: identity.RoleEmployee, Status: identity.StatusSuspended}
	_ = users.Create(context.Background(), suspended)
	dir.byEmail["sus@tsmc.com"] = &identity.EmployeeDirectoryEntry{EmployeeID: "E2", PrimaryEmail: "sus@tsmc.com", DisplayName: "Sus", Status: identity.StatusActive}
	st.m["S5"] = &oidc.StatePayload{App: "employee", Provider: "google", PKCEVerifier: "v", Nonce: "n"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-005", Email: "sus@tsmc.com", EmailVerified: true}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "employee", State: "S5", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrAccountSuspended)
}

func TestService_CompleteLogin_StateExpired(t *testing.T) {
	svc, _, _, _, _, _, _, _, _, _ := buildSvc()
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "employee", State: "missing", Code: "C"})
	assert.ErrorIs(t, err, oidc.ErrStateNotFound)
}

func TestService_CompleteLogin_VendorInviteMissingInviteCode(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, _ := buildSvc()
	st.m["S6"] = &oidc.StatePayload{App: "merchant", Provider: "google", PKCEVerifier: "v", Nonce: "n"}
	g.userinfo = &oidc.Userinfo{Provider: "google", ExternalSubject: "g-006", Email: "vendor@x.com", EmailVerified: true}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{App: "merchant", State: "S6", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrInviteNotFound)
}

func TestService_CompleteLogin_VendorInviteHappy(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, invs := buildSvc()
	require.NoError(t, invs.Put(context.Background(), &identity.VendorInvite{
		Code:      "TBI-OK",
		VendorID:  "vendor-id-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}))
	st.m["S-merchant"] = &oidc.StatePayload{
		App: "merchant", Provider: "google",
		PKCEVerifier: "v", Nonce: "n",
		InviteCode: "TBI-OK",
	}
	g.userinfo = &oidc.Userinfo{
		Provider: "google", ExternalSubject: "g-merchant",
		Email: "merchant@vendor.tw", EmailVerified: true,
		DisplayName: "M",
	}
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{
		App: "merchant", State: "S-merchant", Code: "C",
	})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleVendorOperator, out.User.Role)
	require.NotNil(t, out.User.VendorID)
	assert.Equal(t, "vendor-id-1", *out.User.VendorID)
	// Invite is now consumed.
	inv, err := invs.Get(context.Background(), "TBI-OK")
	require.NoError(t, err)
	require.NotNil(t, inv.ConsumedAt)
	require.NotNil(t, inv.ConsumedBy)
	assert.Equal(t, out.User.ID, *inv.ConsumedBy)
}

func TestService_CompleteLogin_VendorInviteExpired(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, invs := buildSvc()
	require.NoError(t, invs.Put(context.Background(), &identity.VendorInvite{
		Code:      "TBI-EXP",
		VendorID:  "vendor-id-2",
		ExpiresAt: time.Now().Add(-time.Hour),
	}))
	st.m["S-exp"] = &oidc.StatePayload{
		App: "merchant", Provider: "google",
		PKCEVerifier: "v", Nonce: "n",
		InviteCode: "TBI-EXP",
	}
	g.userinfo = &oidc.Userinfo{
		Provider: "google", ExternalSubject: "g-exp",
		Email: "exp@vendor.tw", EmailVerified: true,
	}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{
		App: "merchant", State: "S-exp", Code: "C",
	})
	assert.ErrorIs(t, err, identity.ErrInviteExpired)
}

func TestService_CompleteLogin_VendorInviteAlreadyConsumed(t *testing.T) {
	svc, _, _, _, _, _, st, g, _, invs := buildSvc()
	consumedAt := time.Now().Add(-time.Minute)
	consumedBy := "some-prior-user"
	require.NoError(t, invs.Put(context.Background(), &identity.VendorInvite{
		Code:       "TBI-USED",
		VendorID:   "vendor-id-3",
		ExpiresAt:  time.Now().Add(time.Hour),
		ConsumedAt: &consumedAt,
		ConsumedBy: &consumedBy,
	}))
	st.m["S-used"] = &oidc.StatePayload{
		App: "merchant", Provider: "google",
		PKCEVerifier: "v", Nonce: "n",
		InviteCode: "TBI-USED",
	}
	g.userinfo = &oidc.Userinfo{
		Provider: "google", ExternalSubject: "g-used",
		Email: "used@vendor.tw", EmailVerified: true,
	}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{
		App: "merchant", State: "S-used", Code: "C",
	})
	assert.ErrorIs(t, err, identity.ErrInviteAlreadyUsed)
}
