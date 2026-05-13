package identity

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

type Service struct {
	Users      UserRepository
	Identities UserIdentityRepository
	Directory  EmployeeDirectoryRepository
	Invites    VendorInviteRepository
	AdminWL    AdminWhitelistRepository
	Sessions   SessionStore
	Providers  map[string]oidc.Provider
	States     oidc.StateStore
	Clock      Clock
}

type Clock interface{ Now() time.Time }

type StartLoginInput struct {
	App        string
	Provider   string
	ReturnTo   string
	InviteCode string
}

type StartLoginOutput struct {
	AuthURL string
	State   string
}

type CompleteLoginInput struct {
	App   string
	State string
	Code  string
}

type CompleteLoginOutput struct {
	User     *User
	Session  *Session
	ReturnTo string
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
		InviteCode:   in.InviteCode,
	}
	if err := s.States.Put(ctx, state, payload); err != nil {
		return nil, err
	}
	return &StartLoginOutput{AuthURL: au.URL, State: state}, nil
}

func (s *Service) CompleteLogin(ctx context.Context, in CompleteLoginInput) (*CompleteLoginOutput, error) {
	sp, err := s.States.Get(ctx, in.State)
	if err != nil {
		return nil, err
	}
	if sp.App != in.App {
		return nil, fmt.Errorf("identity: state app mismatch (got %s, want %s)", sp.App, in.App)
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
		return nil, fmt.Errorf("identity: email not verified by provider")
	}
	email := ui.Email

	user, err := s.Users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}

	role := roleForApp(in.App)
	if user == nil {
		u, err := s.bootstrapUser(ctx, role, email, ui.DisplayName, sp.InviteCode)
		if err != nil {
			return nil, err
		}
		user = u
	} else {
		if user.Role != role {
			return nil, fmt.Errorf("identity: user role %s does not match app %s", user.Role, in.App)
		}
	}

	if user.Status == StatusSuspended || user.Status == StatusTerminated {
		return nil, ErrAccountSuspended
	}

	// Link identity if not yet linked
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
	return &CompleteLoginOutput{User: user, Session: sess, ReturnTo: sp.ReturnTo}, nil
}

func (s *Service) bootstrapUser(ctx context.Context, role Role, email, name, inviteCode string) (*User, error) {
	switch role {
	case RoleEmployee:
		entry, err := s.Directory.GetByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		if entry.Status != StatusActive {
			return nil, ErrAccountSuspended
		}
		u := &User{
			PrimaryEmail: email,
			DisplayName:  entry.DisplayName,
			Role:         RoleEmployee,
			Status:       StatusActive,
			EmployeeID:   &entry.EmployeeID,
			Plant:        entry.Plant,
			Department:   entry.Department,
		}
		if err := s.Users.Create(ctx, u); err != nil {
			return nil, err
		}
		return u, nil

	case RoleWelfareAdmin:
		ok, err := s.AdminWL.IsAllowed(ctx, email)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrNotInAdminWhitelist
		}
		u := &User{
			PrimaryEmail: email,
			DisplayName:  name,
			Role:         RoleWelfareAdmin,
			Status:       StatusActive,
		}
		if err := s.Users.Create(ctx, u); err != nil {
			return nil, err
		}
		return u, nil

	case RoleVendorOperator:
		if inviteCode == "" {
			return nil, ErrInviteNotFound
		}
		inv, err := s.Invites.Get(ctx, inviteCode)
		if err != nil {
			return nil, err
		}
		if inv.ConsumedAt != nil {
			return nil, ErrInviteAlreadyUsed
		}
		if inv.ExpiresAt.Before(s.Clock.Now()) {
			return nil, ErrInviteExpired
		}
		// NOTE: P2 does this in 3 separate calls (no transaction). A failure between
		// Users.Create and Invites.Consume leaves an orphaned user. Acceptable for P2.
		// P3+ should wrap in a transaction via a unit-of-work pattern.
		u := &User{
			PrimaryEmail: email,
			DisplayName:  name,
			Role:         RoleVendorOperator,
			Status:       StatusActive,
			VendorID:     &inv.VendorID,
		}
		if err := s.Users.Create(ctx, u); err != nil {
			return nil, err
		}
		if err := s.Invites.Consume(ctx, inviteCode, u.ID); err != nil {
			return nil, err
		}
		return u, nil

	default:
		return nil, ErrInvalidRole
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
