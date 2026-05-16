package identity_test

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

// ---- User ----
type fakeUserRepo struct {
	mu     sync.Mutex
	users  map[string]*identity.User // by email
	byID   map[string]*identity.User
	nextID int
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{users: map[string]*identity.User{}, byID: map[string]*identity.User{}}
}

func (r *fakeUserRepo) Create(ctx context.Context, u *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	u.ID = fmtInt(r.nextID)
	u.CreatedAt = time.Now().UTC()
	u.UpdatedAt = u.CreatedAt
	r.users[u.PrimaryEmail] = u
	r.byID[u.ID] = u
	return nil
}

func (r *fakeUserRepo) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.users[email]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) GetByID(ctx context.Context, id string) (*identity.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id]; ok {
		return u, nil
	}
	return nil, identity.ErrUserNotFound
}

func (r *fakeUserRepo) UpdateStatus(ctx context.Context, id string, status identity.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u, ok := r.byID[id]; ok {
		u.Status = status
		return nil
	}
	return identity.ErrUserNotFound
}

func (r *fakeUserRepo) UpdateProfile(ctx context.Context, u *identity.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.byID[u.ID]
	if !ok {
		return identity.ErrUserNotFound
	}
	delete(r.users, existing.PrimaryEmail)
	u.CreatedAt = existing.CreatedAt
	u.UpdatedAt = time.Now().UTC()
	r.users[u.PrimaryEmail] = u
	r.byID[u.ID] = u
	return nil
}

// ---- Identity ----
type fakeIdentityRepo struct {
	mu    sync.Mutex
	bySub map[string]*identity.UserIdentity // key = provider+":"+sub
}

func newFakeIdentityRepo() *fakeIdentityRepo {
	return &fakeIdentityRepo{bySub: map[string]*identity.UserIdentity{}}
}

func (r *fakeIdentityRepo) GetByProviderSubject(ctx context.Context, p identity.Provider, sub string) (*identity.UserIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if x, ok := r.bySub[string(p)+":"+sub]; ok {
		return x, nil
	}
	return nil, identity.ErrIdentityNotFound
}

func (r *fakeIdentityRepo) Link(ctx context.Context, ui *identity.UserIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ui.ID = "ui-" + ui.ExternalSubject
	ui.LinkedAt = time.Now().UTC()
	r.bySub[string(ui.Provider)+":"+ui.ExternalSubject] = ui
	return nil
}

func (r *fakeIdentityRepo) ListByUser(ctx context.Context, userID string) ([]*identity.UserIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*identity.UserIdentity
	for _, x := range r.bySub {
		if x.UserID == userID {
			out = append(out, x)
		}
	}
	return out, nil
}

// ---- Session ----
type fakeSessions struct {
	mu      sync.Mutex
	byToken map[string]*identity.Session
	nextID  int
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byToken: map[string]*identity.Session{}}
}

func (r *fakeSessions) Create(ctx context.Context, userID string, role identity.Role) (*identity.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	s := &identity.Session{
		Token:      "tb_test_" + fmtInt(r.nextID),
		UserID:     userID,
		Role:       role,
		CreatedAt:  time.Now().UTC(),
		LastSeenAt: time.Now().UTC(),
	}
	r.byToken[s.Token] = s
	return s, nil
}

func (r *fakeSessions) Get(ctx context.Context, token string) (*identity.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.byToken[token]; ok {
		return s, nil
	}
	return nil, identity.ErrSessionNotFound
}

func (r *fakeSessions) Touch(ctx context.Context, token string) error { return nil }

func (r *fakeSessions) Revoke(ctx context.Context, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byToken, token)
	return nil
}

func (r *fakeSessions) RevokeAllForUser(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for tok, s := range r.byToken {
		if s.UserID == userID {
			delete(r.byToken, tok)
		}
	}
	return nil
}

// ---- State store ----
type fakeStates struct {
	mu sync.Mutex
	m  map[string]*oidc.StatePayload
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

// ---- Provider ----
type fakeProvider struct {
	name      string
	userinfo  *oidc.Userinfo
	nextState string
	err       error
}

func (p *fakeProvider) Name() string { return p.name }

func (p *fakeProvider) DisplayName() string { return p.name }

func (p *fakeProvider) BuildAuthURL(ctx context.Context, state string) (*oidc.AuthURL, error) {
	p.nextState = state
	return &oidc.AuthURL{URL: "https://fake/" + state, PKCEVerifier: "v", Nonce: "n"}, nil
}

func (p *fakeProvider) Exchange(ctx context.Context, code, _v, _n string) (*oidc.Userinfo, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.userinfo, nil
}

// ---- helpers ----
func fmtInt(n int) string { return strconv.Itoa(n) }
