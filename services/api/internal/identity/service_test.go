package identity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

func buildSvc() (*identity.Service, *fakeUserRepo, *fakeIdentityRepo, *fakeSessions, *fakeStates, *fakeProvider) {
	users := newFakeUserRepo()
	ids := newFakeIdentityRepo()
	sess := newFakeSessions()
	st := newFakeStates()
	p := &fakeProvider{name: "authentik"}
	svc := &identity.Service{
		Users:      users,
		Identities: ids,
		Sessions:   sess,
		Providers:  map[string]oidc.Provider{"authentik": p},
		States:     st,
	}
	return svc, users, ids, sess, st, p
}

func TestService_StartLogin_HappyAuthentik(t *testing.T) {
	svc, _, _, _, _, _ := buildSvc()
	out, err := svc.StartLogin(context.Background(), identity.StartLoginInput{
		App: "employee", Provider: "authentik", ReturnTo: "/menu",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, out.AuthURL)
	assert.NotEmpty(t, out.State)
}

func TestService_StartLogin_InvalidProvider(t *testing.T) {
	svc, _, _, _, _, _ := buildSvc()
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "employee", Provider: "google"})
	assert.ErrorIs(t, err, identity.ErrInvalidProvider)
}

func TestService_ProviderInfos(t *testing.T) {
	svc, _, _, _, _, _ := buildSvc()
	infos := svc.ProviderInfos()
	require.Len(t, infos, 1)
	assert.Equal(t, "authentik", infos[0].Slug)
}

func TestService_CompleteLogin_EmployeeHappy(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S1"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n", ReturnTo: "/"}
	p.userinfo = employeeInfo("ak-001", "alice@tbite.test")
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S1", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleEmployee, out.User.Role)
	assert.Equal(t, "E001", *out.User.EmployeeID)
	assert.Equal(t, "F12B-3F", *out.User.Plant)
	assert.NotEmpty(t, out.Session.Token)
	assert.Equal(t, "employee", out.App)
}

func TestService_CompleteLogin_AdminHappy(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S2"] = &oidc.StatePayload{App: "admin", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = userInfo("ak-admin", "root@tbite.test", identity.RoleWelfareAdmin, nil)
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S2", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleWelfareAdmin, out.User.Role)
}

func TestService_CompleteLogin_VendorOperatorHappy(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S3"] = &oidc.StatePayload{App: "merchant", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = userInfo("ak-vendor", "operator@vendor.tw", identity.RoleVendorOperator, map[string]any{
		"tbite_vendor_id": "a1111111-1111-1111-1111-111111111111",
	})
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S3", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleVendorOperator, out.User.Role)
	require.NotNil(t, out.User.VendorID)
	assert.Equal(t, "a1111111-1111-1111-1111-111111111111", *out.User.VendorID)
}

func TestService_CompleteLogin_MissingRoleRejected(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S4"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = &oidc.Userinfo{
		Provider: "authentik", ExternalSubject: "ak-no-role",
		Email: "x@tbite.test", EmailVerified: true,
		DisplayName: "X", Raw: map[string]any{},
	}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S4", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrInvalidClaims)
}

func TestService_CompleteLogin_RoleAppMismatchRejected(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S5"] = &oidc.StatePayload{App: "admin", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-employee", "employee@tbite.test")
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S5", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrRoleMismatch)
}

func TestService_CompleteLogin_MissingVendorClaimRejected(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S6"] = &oidc.StatePayload{App: "merchant", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = userInfo("ak-vendor-missing", "operator@vendor.tw", identity.RoleVendorOperator, nil)
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S6", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrInvalidClaims)
}

func TestService_CompleteLogin_StateProviderMismatch(t *testing.T) {
	svc, _, _, _, st, p := buildSvc()
	st.m["S7"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-001", "alice@tbite.test")
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "other", State: "S7", Code: "C"})
	assert.Error(t, err)
}

func TestService_CompleteLogin_SuspendedLocalUserRejected(t *testing.T) {
	svc, users, _, _, st, p := buildSvc()
	suspended := &identity.User{
		PrimaryEmail: "sus@tbite.test", DisplayName: "Sus",
		Role: identity.RoleEmployee, Status: identity.StatusSuspended,
	}
	require.NoError(t, users.Create(context.Background(), suspended))
	st.m["S8"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-sus", "sus@tbite.test")
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S8", Code: "C"})
	assert.ErrorIs(t, err, identity.ErrAccountSuspended)
}

func TestService_CompleteLogin_StateExpired(t *testing.T) {
	svc, _, _, _, _, _ := buildSvc()
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "missing", Code: "C"})
	assert.ErrorIs(t, err, oidc.ErrStateNotFound)
}

func TestService_CompleteLogin_StateAlreadyConsumedKeepsAppContext(t *testing.T) {
	svc, _, _, _, st, _ := buildSvc()
	st.consumed["S9"] = &oidc.StatePayload{App: "merchant", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}

	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S9", Code: "C"})
	require.Error(t, err)
	assert.ErrorIs(t, err, oidc.ErrStateConsumed)
	var cbErr *identity.CallbackError
	require.ErrorAs(t, err, &cbErr)
	assert.Equal(t, "merchant", cbErr.App)
}

func employeeInfo(sub, email string) *oidc.Userinfo {
	return userInfo(sub, email, identity.RoleEmployee, map[string]any{
		"tbite_employee_id": "E001",
		"tbite_plant":       "F12B-3F",
		"tbite_department":  "IT",
	})
}

func userInfo(sub, email string, role identity.Role, extra map[string]any) *oidc.Userinfo {
	raw := map[string]any{"tbite_role": string(role)}
	for k, v := range extra {
		raw[k] = v
	}
	return &oidc.Userinfo{
		Provider:        "authentik",
		ExternalSubject: sub,
		Email:           email,
		EmailVerified:   true,
		DisplayName:     email,
		Raw:             raw,
	}
}
