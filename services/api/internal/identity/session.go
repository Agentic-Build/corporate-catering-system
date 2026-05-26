package identity

import (
	"context"
	"errors"
	"time"
)

type Session struct {
	Token      string
	UserID     string
	Role       Role
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type SessionStore interface {
	Create(ctx context.Context, userID string, role Role) (*Session, error)
	Get(ctx context.Context, token string) (*Session, error)
	Touch(ctx context.Context, token string) error
	Revoke(ctx context.Context, token string) error
	RevokeAllForUser(ctx context.Context, userID string) error
}

// AuthHandoffStore brokers a short-lived, single-use code that stands in for a
// freshly-minted session token during the OIDC login redirect, so the token
// never travels in a URL (browser history / proxy logs). The app server
// redeems the code over its trusted server-to-API channel.
type AuthHandoffStore interface {
	IssueCode(ctx context.Context, token string) (string, error)
	// RedeemCode returns the token for a valid code and invalidates it
	// (single use). It errors when the code is unknown or already redeemed.
	RedeemCode(ctx context.Context, code string) (string, error)
}

// ErrHandoffNotFound is returned by RedeemCode for an unknown/expired/used code.
var ErrHandoffNotFound = errors.New("identity: auth handoff code not found")
