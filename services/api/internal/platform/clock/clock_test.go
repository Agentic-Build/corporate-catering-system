package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
)

func TestSystemClock_NowIsUTCAndMonotonic(t *testing.T) {
	c := clock.SystemClock{}
	t0 := c.Now()
	time.Sleep(time.Millisecond)
	t1 := c.Now()
	assert.Equal(t, "UTC", t0.Location().String())
	assert.True(t, t1.After(t0))
}

func TestFixedClock_ReturnsFixed(t *testing.T) {
	fixed := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	c := clock.FixedClock{T: fixed}
	assert.Equal(t, fixed, c.Now())
}
