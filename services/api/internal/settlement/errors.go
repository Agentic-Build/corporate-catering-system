package settlement

import "errors"

var (
	ErrSettlementNotFound  = errors.New("settlement: settlement not found")
	ErrPeriodAlreadyClosed = errors.New("settlement: an active settlement already exists for this period")
	ErrInvalidPeriod       = errors.New("settlement: period_start must be <= period_end")
	ErrNoOrdersInPeriod    = errors.New("settlement: no orders to settle in this period")
	ErrInvalidTransition   = errors.New("settlement: invalid state transition")
	ErrForbidden           = errors.New("settlement: forbidden")
)
