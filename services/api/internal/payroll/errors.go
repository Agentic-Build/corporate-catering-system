package payroll

import "errors"

var (
	ErrBatchNotFound     = errors.New("payroll: batch not found")
	ErrEntryNotFound     = errors.New("payroll: entry not found")
	ErrDisputeNotFound   = errors.New("payroll: dispute not found")
	ErrBatchLocked       = errors.New("payroll: batch is already locked")
	ErrBatchPeriodExists = errors.New("payroll: batch for this period already exists")
	ErrInvalidTransition = errors.New("payroll: invalid state transition")
	ErrForbidden         = errors.New("payroll: forbidden")
	ErrExceptionNotFound = errors.New("payroll: exception not found")
	ErrInvalidException  = errors.New("payroll: invalid exception request")
)
