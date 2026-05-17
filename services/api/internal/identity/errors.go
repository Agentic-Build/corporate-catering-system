package identity

import "errors"

var (
	ErrUserNotFound     = errors.New("identity: user not found")
	ErrIdentityNotFound = errors.New("identity: external identity not found")
	ErrInvalidClaims    = errors.New("identity: invalid provider claims")
	ErrRoleMismatch     = errors.New("identity: provider role does not match app")
	ErrAccountSuspended = errors.New("identity: account suspended")
	ErrInvalidProvider  = errors.New("identity: invalid provider")
	ErrInvalidRole      = errors.New("identity: invalid role")
	ErrSessionNotFound  = errors.New("identity: session not found")
)
