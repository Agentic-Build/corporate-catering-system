package identity

import "context"

type UserRepository interface {
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Create(ctx context.Context, u *User) error
	UpdateStatus(ctx context.Context, id string, status Status) error
}

type UserIdentityRepository interface {
	GetByProviderSubject(ctx context.Context, p Provider, sub string) (*UserIdentity, error)
	Link(ctx context.Context, ui *UserIdentity) error
	ListByUser(ctx context.Context, userID string) ([]*UserIdentity, error)
}

type EmployeeDirectoryRepository interface {
	GetByEmail(ctx context.Context, email string) (*EmployeeDirectoryEntry, error)
}

type VendorInviteRepository interface {
	Get(ctx context.Context, code string) (*VendorInvite, error)
	Consume(ctx context.Context, code string, userID string) error
}

type AdminWhitelistRepository interface {
	IsAllowed(ctx context.Context, email string) (bool, error)
}
