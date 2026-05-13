package identity

import (
	"context"
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
