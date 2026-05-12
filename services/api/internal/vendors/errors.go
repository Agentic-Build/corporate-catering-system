package vendor

import "errors"

var (
	ErrVendorNotFound  = errors.New("vendor: not found")
	ErrAlreadyApproved = errors.New("vendor: already approved")
	ErrInvalidStatus   = errors.New("vendor: invalid status transition")
)
