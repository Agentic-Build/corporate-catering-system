package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

type Service struct {
	Users      UserRepository
	Identities UserIdentityRepository
	Sessions   SessionStore
	Providers  map[string]oidc.Provider
	States     oidc.StateStore
}

type StartLoginInput struct {
	App      string
	Provider string
	ReturnTo string
}

type StartLoginOutput struct {
	AuthURL string
	State   string
}

type CompleteLoginInput struct {
	Provider string
	State    string
	Code     string
}

type CompleteLoginOutput struct {
	User     *User
	Session  *Session
	App      string
	ReturnTo string
}

func (s *Service) ProviderInfos() []oidc.ProviderInfo {
	out := make([]oidc.ProviderInfo, 0, len(s.Providers))
	for slug, p := range s.Providers {
		out = append(out, oidc.ProviderInfo{Slug: slug, DisplayName: p.DisplayName()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

func (s *Service) StartLogin(ctx context.Context, in StartLoginInput) (*StartLoginOutput, error) {
	if !validApp(in.App) {
		return nil, fmt.Errorf("identity: invalid app %q", in.App)
	}
	p, ok := s.Providers[in.Provider]
	if !ok {
		return nil, ErrInvalidProvider
	}
	state := randState()
	au, err := p.BuildAuthURL(ctx, state)
	if err != nil {
		return nil, err
	}
	payload := &oidc.StatePayload{
		App:          in.App,
		Provider:     in.Provider,
		ReturnTo:     safeReturnTo(in.ReturnTo),
		PKCEVerifier: au.PKCEVerifier,
		Nonce:        au.Nonce,
	}
	if err := s.States.Put(ctx, state, payload); err != nil {
		return nil, err
	}
	return &StartLoginOutput{AuthURL: au.URL, State: state}, nil
}

func (s *Service) CompleteLogin(ctx context.Context, in CompleteLoginInput) (out *CompleteLoginOutput, err error) {
	sp, err := s.States.Get(ctx, in.State)
	if err != nil {
		if errors.Is(err, oidc.ErrStateConsumed) && sp != nil {
			return nil, &CallbackError{App: sp.App, Err: err}
		}
		return nil, err
	}
	// Once the state is resolved we know which app the user was entering, so
	// wrap any later failure with that context. The browser callback uses it
	// to redirect to the app's login page instead of dumping raw JSON.
	defer func() {
		if err != nil {
			err = &CallbackError{App: sp.App, Err: err}
		}
	}()
	if sp.Provider != in.Provider {
		return nil, fmt.Errorf("identity: state provider mismatch (got %s, want %s)", in.Provider, sp.Provider)
	}
	_ = s.States.Consume(ctx, in.State)

	p, ok := s.Providers[sp.Provider]
	if !ok {
		return nil, ErrInvalidProvider
	}
	ui, err := p.Exchange(ctx, in.Code, sp.PKCEVerifier, sp.Nonce)
	if err != nil {
		return nil, err
	}
	if !ui.EmailVerified {
		return nil, fmt.Errorf("%w: email not verified", ErrInvalidClaims)
	}
	if ui.Email == "" || ui.ExternalSubject == "" {
		return nil, fmt.Errorf("%w: missing subject or email", ErrInvalidClaims)
	}

	claims, err := userFromClaims(sp.App, ui)
	if err != nil {
		return nil, err
	}

	user, err := s.userForIdentity(ctx, Provider(sp.Provider), ui.ExternalSubject, claims.PrimaryEmail)
	if err != nil {
		return nil, err
	}
	if user == nil {
		user = claims
		if err := s.Users.Create(ctx, user); err != nil {
			return nil, err
		}
	} else {
		if user.Status != StatusActive {
			return nil, ErrAccountSuspended
		}
		claims.ID = user.ID
		user = claims
		if err := s.Users.UpdateProfile(ctx, user); err != nil {
			return nil, err
		}
	}

	if _, err := s.Identities.GetByProviderSubject(ctx, Provider(sp.Provider), ui.ExternalSubject); err != nil {
		if !errors.Is(err, ErrIdentityNotFound) {
			return nil, err
		}
		if err := s.Identities.Link(ctx, &UserIdentity{
			UserID:          user.ID,
			Provider:        Provider(sp.Provider),
			ExternalSubject: ui.ExternalSubject,
			RawClaims:       ui.Raw,
		}); err != nil {
			return nil, err
		}
	}

	sess, err := s.Sessions.Create(ctx, user.ID, user.Role)
	if err != nil {
		return nil, err
	}
	return &CompleteLoginOutput{User: user, Session: sess, App: sp.App, ReturnTo: sp.ReturnTo}, nil
}

func (s *Service) userForIdentity(ctx context.Context, provider Provider, subject, email string) (*User, error) {
	ui, err := s.Identities.GetByProviderSubject(ctx, provider, subject)
	if err == nil {
		return s.Users.GetByID(ctx, ui.UserID)
	}
	if !errors.Is(err, ErrIdentityNotFound) {
		return nil, err
	}
	user, err := s.Users.GetByEmail(ctx, email)
	if errors.Is(err, ErrUserNotFound) {
		return nil, nil
	}
	return user, err
}

func userFromClaims(app string, ui *oidc.Userinfo) (*User, error) {
	role := Role(claimString(ui.Raw, "tbite_role"))
	if !validRole(role) {
		return nil, fmt.Errorf("%w: missing or invalid tbite_role", ErrInvalidClaims)
	}
	want := roleForApp(app)
	if role != want {
		return nil, fmt.Errorf("%w: role %s cannot enter %s", ErrRoleMismatch, role, app)
	}

	u := &User{
		PrimaryEmail: strings.ToLower(ui.Email),
		DisplayName:  ui.DisplayName,
		Role:         role,
		Status:       StatusActive,
	}
	switch role {
	case RoleEmployee:
		employeeID := claimString(ui.Raw, "tbite_employee_id")
		plant := claimString(ui.Raw, "tbite_plant")
		department := claimString(ui.Raw, "tbite_department")
		if employeeID == "" || plant == "" {
			return nil, fmt.Errorf("%w: employee claims require tbite_employee_id and tbite_plant", ErrInvalidClaims)
		}
		u.EmployeeID = &employeeID
		u.Plant = &plant
		if department != "" {
			u.Department = &department
		}
	case RoleVendorOperator:
		vendorID := claimString(ui.Raw, "tbite_vendor_id")
		if vendorID == "" {
			return nil, fmt.Errorf("%w: vendor operator requires tbite_vendor_id", ErrInvalidClaims)
		}
		u.VendorID = &vendorID
	case RoleWelfareAdmin:
	default:
		return nil, ErrInvalidRole
	}
	return u, nil
}

func claimString(claims map[string]any, key string) string {
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}

func roleForApp(app string) Role {
	switch app {
	case "employee":
		return RoleEmployee
	case "merchant":
		return RoleVendorOperator
	case "admin":
		return RoleWelfareAdmin
	default:
		return ""
	}
}

func validApp(app string) bool {
	return app == "employee" || app == "merchant" || app == "admin"
}

func validRole(role Role) bool {
	return role == RoleEmployee || role == RoleVendorOperator || role == RoleWelfareAdmin
}

func safeReturnTo(s string) string {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return "/"
	}
	return s
}

func randState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
