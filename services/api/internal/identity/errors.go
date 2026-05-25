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

// CallbackError wraps a failure that occurred after the OIDC state was resolved,
// carrying the app the user was trying to enter so the browser callback can
// redirect to that app instead of returning raw JSON.
type CallbackError struct {
	App string
	Err error
}

func (e *CallbackError) Error() string { return e.Err.Error() }
func (e *CallbackError) Unwrap() error { return e.Err }
