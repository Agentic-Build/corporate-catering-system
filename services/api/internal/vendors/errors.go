package vendor

import "errors"

var (
	ErrVendorNotFound    = errors.New("vendor: not found")
	ErrOperatorNotFound  = errors.New("vendor: operator not found")
	ErrAlreadyApproved   = errors.New("vendor: already approved")
	ErrInvalidStatus     = errors.New("vendor: invalid status transition")
	ErrInvalidOperator   = errors.New("vendor: invalid operator")
	ErrProvisioningSetup = errors.New("vendor: authentik provisioning failed")
)
