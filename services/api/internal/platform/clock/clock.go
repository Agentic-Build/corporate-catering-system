package clock

import "time"

// Nower returns the current time. Single-method abstraction so tests can pin "now".
type Nower interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type FixedClock struct{ T time.Time }

func (c FixedClock) Now() time.Time { return c.T }
