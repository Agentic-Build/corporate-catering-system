package httpserver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTimePtr(t *testing.T) {
	t.Run("zero returns nil", func(t *testing.T) {
		assert.Nil(t, FormatTimePtr(time.Time{}))
	})

	t.Run("non-zero returns RFC3339 UTC", func(t *testing.T) {
		ts := time.Date(2026, 5, 31, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
		got := FormatTimePtr(ts)
		require.NotNil(t, got)
		assert.Equal(t, "2026-05-31T00:00:00Z", *got)
	})
}

func TestFormatNullableTimePtr(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		assert.Nil(t, FormatNullableTimePtr(nil))
	})

	t.Run("zero returns nil", func(t *testing.T) {
		z := time.Time{}
		assert.Nil(t, FormatNullableTimePtr(&z))
	})

	t.Run("non-zero returns RFC3339 UTC", func(t *testing.T) {
		ts := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
		got := FormatNullableTimePtr(&ts)
		require.NotNil(t, got)
		assert.Equal(t, "2026-05-31T00:00:00Z", *got)
	})
}
