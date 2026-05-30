package httpserver

import (
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMap(t *testing.T) {
	errNotFound := errors.New("thing not found")
	errConflict := errors.New("already exists")
	rules := []Rule{
		{Err: errNotFound, Status: http.StatusNotFound},
		{Err: errConflict, Status: http.StatusConflict, Detail: "duplicate"},
	}

	t.Run("nil error returns nil", func(t *testing.T) {
		assert.NoError(t, Map(nil, rules))
	})

	t.Run("no match returns nil", func(t *testing.T) {
		assert.NoError(t, Map(errors.New("unmapped"), rules))
	})

	t.Run("match uses err.Error when Detail empty", func(t *testing.T) {
		err := Map(errNotFound, rules)
		require.Error(t, err)
		var se huma.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusNotFound, se.GetStatus())
		assert.Equal(t, "thing not found", err.Error())
	})

	t.Run("match uses Detail override", func(t *testing.T) {
		wrapped := errors.Join(errConflict, errors.New("ctx"))
		err := Map(wrapped, rules)
		require.Error(t, err)
		var se huma.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusConflict, se.GetStatus())
		assert.Equal(t, "duplicate", err.Error())
	})
}
