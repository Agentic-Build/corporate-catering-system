package feedback

import "errors"

var (
	ErrOrderNotFound     = errors.New("feedback: order not found")
	ErrComplaintNotFound = errors.New("feedback: complaint not found")
	ErrRatingNotFound    = errors.New("feedback: rating not found")
	ErrOrderNotPickedUp  = errors.New("feedback: order is not in picked_up status")
	ErrAlreadyRated      = errors.New("feedback: order already has a rating")
	ErrComplaintExists   = errors.New("feedback: order already has an unresolved complaint")
	ErrInvalidTransition = errors.New("feedback: invalid complaint state transition")
	ErrEscalateTooEarly  = errors.New("feedback: complaint cannot be escalated before 24h since creation")
	ErrForbidden         = errors.New("feedback: forbidden")
	ErrValidation        = errors.New("feedback: validation failed")
)
