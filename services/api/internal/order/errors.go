package order

import "errors"

var (
	ErrOrderNotFound       = errors.New("order: not found")
	ErrInvalidTransition   = errors.New("order: invalid state transition")
	ErrCutoffPassed        = errors.New("order: cutoff time has passed")
	ErrEmptyOrder          = errors.New("order: must contain at least one item")
	ErrPlantMismatch       = errors.New("order: plant does not match user")
	ErrVendorPlantMismatch = errors.New("order: vendor does not serve this plant")
	ErrForbidden           = errors.New("order: forbidden")
)
