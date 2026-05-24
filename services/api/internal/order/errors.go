package order

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrOrderNotFound       = errors.New("order: not found")
	ErrInvalidTransition   = errors.New("order: invalid state transition")
	ErrCutoffPassed        = errors.New("order: cutoff time has passed")
	ErrEmptyOrder          = errors.New("order: must contain at least one item")
	ErrMultiVendor         = errors.New("order: items must be from the same vendor")
	ErrPlantMismatch       = errors.New("order: plant does not match user")
	ErrVendorPlantMismatch = errors.New("order: vendor does not serve this plant")
	ErrForbidden           = errors.New("order: forbidden")
	// ErrConcurrentModification is returned when a Postgres deadlock (40P01)
	// or serialization failure (40001) aborts the order transaction. Both are
	// retryable from the caller's perspective and map to HTTP 409, not 500.
	ErrConcurrentModification = errors.New("order: concurrent modification, please retry")
)

// MaybeConcurrencyErr inspects err for retryable Postgres conflict codes
// (40P01 deadlock_detected, 40001 serialization_failure) and wraps them as
// ErrConcurrentModification. Other errors pass through unchanged.
//
// Use at the boundary of every pgx.BeginFunc that touches the order table —
// deadlocks happen when modify and cancel race on the same row, and the
// caller deserves a 409 with a "please retry" hint instead of an opaque 500.
func MaybeConcurrencyErr(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "40P01", "40001":
			return fmt.Errorf("%w: %s", ErrConcurrentModification, pg.Message)
		}
	}
	return err
}
