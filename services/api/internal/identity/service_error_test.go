package identity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

var errBoom = errors.New("boom")

// === error-injecting decorators around the existing fakes ===

type errUserRepo struct {
	*fakeUserRepo
	getByIDErr  error
	getEmailErr error
	createErr   error
	updateErr   error
}

func (r *errUserRepo) GetByID(ctx context.Context, id string) (*identity.User, error) {
	if r.getByIDErr != nil {
		return nil, r.getByIDErr
	}
	return r.fakeUserRepo.GetByID(ctx, id)
}

func (r *errUserRepo) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	if r.getEmailErr != nil {
		return nil, r.getEmailErr
	}
	return r.fakeUserRepo.GetByEmail(ctx, email)
}

func (r *errUserRepo) Create(ctx context.Context, u *identity.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	return r.fakeUserRepo.Create(ctx, u)
}

func (r *errUserRepo) UpdateProfile(ctx context.Context, u *identity.User) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.fakeUserRepo.UpdateProfile(ctx, u)
}

type errIdentityRepo struct {
	*fakeIdentityRepo
	getErr     error
	linkErr    error
	lookupHook func() error
}

func (r *errIdentityRepo) GetByProviderSubject(ctx context.Context, p identity.Provider, sub string) (*identity.UserIdentity, error) {
	if r.lookupHook != nil {
		if err := r.lookupHook(); err != nil {
			return nil, err
		}
	}
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.fakeIdentityRepo.GetByProviderSubject(ctx, p, sub)
}

func (r *errIdentityRepo) Link(ctx context.Context, ui *identity.UserIdentity) error {
	if r.linkErr != nil {
		return r.linkErr
	}
	return r.fakeIdentityRepo.Link(ctx, ui)
}

type errSessions struct {
	*fakeSessions
	createErr error
}

func (r *errSessions) Create(ctx context.Context, userID string, role identity.Role) (*identity.Session, error) {
	if r.createErr != nil {
		return nil, r.createErr
	}
	return r.fakeSessions.Create(ctx, userID, role)
}

type errStates struct {
	*fakeStates
	putErr error
}

func (s *errStates) Put(ctx context.Context, state string, p *oidc.StatePayload) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.fakeStates.Put(ctx, state, p)
}

// === tests ===

func TestProviderInfos_SortedMultiple(t *testing.T) {
	svc := &identity.Service{
		Providers: map[string]oidc.Provider{
			"zeta":      &fakeProvider{name: "zeta"},
			"authentik": &fakeProvider{name: "authentik"},
			"middle":    &fakeProvider{name: "middle"},
		},
	}
	infos := svc.ProviderInfos()
	require.Len(t, infos, 3)
	assert.Equal(t, "authentik", infos[0].Slug)
	assert.Equal(t, "middle", infos[1].Slug)
	assert.Equal(t, "zeta", infos[2].Slug)
}

func TestStartLogin_InvalidApp(t *testing.T) {
	svc, _, _, _, _, _ := buildSvc()
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "ghost", Provider: "authentik"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid app")
}

func TestStartLogin_BuildAuthURLError(t *testing.T) {
	st := newFakeStates()
	svc := &identity.Service{
		Providers: map[string]oidc.Provider{"authentik": &buildErrProvider{}},
		States:    st,
	}
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "employee", Provider: "authentik"})
	require.ErrorIs(t, err, errBoom)
}

func TestStartLogin_StatePutError(t *testing.T) {
	st := &errStates{fakeStates: newFakeStates(), putErr: errBoom}
	p := &fakeProvider{name: "authentik"}
	svc := &identity.Service{
		Providers: map[string]oidc.Provider{"authentik": p},
		States:    st,
	}
	_, err := svc.StartLogin(context.Background(), identity.StartLoginInput{App: "employee", Provider: "authentik"})
	require.ErrorIs(t, err, errBoom)
}

type buildErrProvider struct{ fakeProvider }

func (p *buildErrProvider) BuildAuthURL(ctx context.Context, state string) (*oidc.AuthURL, error) {
	return nil, errBoom
}

func newErrSvc() (*identity.Service, *errUserRepo, *errIdentityRepo, *errSessions, *fakeStates, *fakeProvider) {
	users := &errUserRepo{fakeUserRepo: newFakeUserRepo()}
	ids := &errIdentityRepo{fakeIdentityRepo: newFakeIdentityRepo()}
	sess := &errSessions{fakeSessions: newFakeSessions()}
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

func TestCompleteLogin_ExchangeError(t *testing.T) {
	svc, _, _, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.err = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_EmailNotVerified(t *testing.T) {
	svc, _, _, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "x@tbite.test")
	p.userinfo.EmailVerified = false
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, identity.ErrInvalidClaims)
	assert.Contains(t, err.Error(), "email not verified")
}

func TestCompleteLogin_MissingEmailOrSubject(t *testing.T) {
	svc, _, _, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("", "x@tbite.test") // empty subject
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, identity.ErrInvalidClaims)
	assert.Contains(t, err.Error(), "missing subject or email")
}

func TestCompleteLogin_UserForIdentityError(t *testing.T) {
	svc, _, ids, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "x@tbite.test")
	ids.getErr = errBoom // GetByProviderSubject returns a non-NotFound error
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_GetByEmailError(t *testing.T) {
	svc, users, _, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "x@tbite.test")
	// identity not found -> falls through to GetByEmail, which errors (non-NotFound).
	users.getEmailErr = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_CreateError(t *testing.T) {
	svc, users, _, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "new@tbite.test")
	users.createErr = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_UpdateProfileError(t *testing.T) {
	svc, users, _, _, st, p := newErrSvc()
	// Pre-existing active user with same email -> upsert hits UpdateProfile.
	require.NoError(t, users.fakeUserRepo.Create(context.Background(), &identity.User{
		PrimaryEmail: "existing@tbite.test", DisplayName: "Old",
		Role: identity.RoleEmployee, Status: identity.StatusActive,
	}))
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "existing@tbite.test")
	users.updateErr = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_ExistingActiveUserUpdatesProfile(t *testing.T) {
	svc, users, ids, _, st, p := newErrSvc()
	require.NoError(t, users.fakeUserRepo.Create(context.Background(), &identity.User{
		PrimaryEmail: "existing@tbite.test", DisplayName: "Old",
		Role: identity.RoleEmployee, Status: identity.StatusActive,
	}))
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "existing@tbite.test")
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, identity.RoleEmployee, out.User.Role)
	// identity should now be linked
	linked, lerr := ids.fakeIdentityRepo.GetByProviderSubject(context.Background(), identity.Provider("authentik"), "ak-1")
	require.NoError(t, lerr)
	assert.Equal(t, out.User.ID, linked.UserID)
}

func TestCompleteLogin_LinkIdentityGetError(t *testing.T) {
	svc, _, ids, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "new@tbite.test")
	// First GetByProviderSubject (in userForIdentity) must NOT error, only the
	// one inside linkIdentityIfMissing should. Use a counter.
	cnt := 0
	ids.getErr = nil
	ids.lookupHook = func() error {
		cnt++
		if cnt >= 2 {
			return errBoom
		}
		return identity.ErrIdentityNotFound
	}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_LinkError(t *testing.T) {
	svc, _, ids, _, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "new@tbite.test")
	ids.linkErr = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_IdentityAlreadyLinked(t *testing.T) {
	svc, users, ids, _, st, p := newErrSvc()
	// Pre-link the identity to an existing active user.
	u := &identity.User{PrimaryEmail: "linked@tbite.test", DisplayName: "L", Role: identity.RoleEmployee, Status: identity.StatusActive}
	require.NoError(t, users.fakeUserRepo.Create(context.Background(), u))
	require.NoError(t, ids.fakeIdentityRepo.Link(context.Background(), &identity.UserIdentity{
		UserID: u.ID, Provider: "authentik", ExternalSubject: "ak-1",
	}))
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "linked@tbite.test")
	out, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.NoError(t, err)
	assert.Equal(t, u.ID, out.User.ID)
}

// TestCompleteLogin_ExchangeProviderMissing reaches the provider-not-found
// branch inside exchangeAndValidate: the state's provider matches the input
// provider (so the mismatch guard passes) but is not registered in the map.
func TestCompleteLogin_ExchangeProviderMissing(t *testing.T) {
	svc, _, _, _, st, _ := newErrSvc() // map only has "authentik"
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "ghost", PKCEVerifier: "v", Nonce: "n"}
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "ghost", State: "S", Code: "C"})
	require.ErrorIs(t, err, identity.ErrInvalidProvider)
}

func TestCompleteLogin_SessionCreateError(t *testing.T) {
	svc, _, _, sess, st, p := newErrSvc()
	st.m["S"] = &oidc.StatePayload{App: "employee", Provider: "authentik", PKCEVerifier: "v", Nonce: "n"}
	p.userinfo = employeeInfo("ak-1", "new@tbite.test")
	sess.createErr = errBoom
	_, err := svc.CompleteLogin(context.Background(), identity.CompleteLoginInput{Provider: "authentik", State: "S", Code: "C"})
	require.ErrorIs(t, err, errBoom)
}
