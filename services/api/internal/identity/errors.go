package identity

import "errors"

var (
	ErrUserNotFound        = errors.New("identity: user not found")
	ErrIdentityNotFound    = errors.New("identity: external identity not found")
	ErrNotInDirectory      = errors.New("identity: email not in employee directory")
	ErrNotInAdminWhitelist = errors.New("identity: email not in admin whitelist")
	ErrInviteNotFound      = errors.New("identity: vendor invite not found")
	ErrInviteAlreadyUsed   = errors.New("identity: vendor invite already consumed")
	ErrInviteExpired       = errors.New("identity: vendor invite expired")
	ErrAccountSuspended    = errors.New("identity: account suspended")
	ErrInvalidProvider     = errors.New("identity: invalid provider")
	ErrInvalidRole         = errors.New("identity: invalid role")
)
