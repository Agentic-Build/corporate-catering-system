package idgen_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/idgen"
)

func TestDefaultGen_NewUUIDIsValid(t *testing.T) {
	g := idgen.DefaultGen{}
	s := g.NewUUID()
	_, err := uuid.Parse(s)
	require.NoError(t, err)
}

func TestDefaultGen_NewTokenLength(t *testing.T) {
	g := idgen.DefaultGen{}
	b, err := g.NewToken(32)
	require.NoError(t, err)
	assert.Len(t, b, 32)
}

func TestDefaultGen_NewTokenIsRandom(t *testing.T) {
	g := idgen.DefaultGen{}
	a, _ := g.NewToken(32)
	b, _ := g.NewToken(32)
	assert.NotEqual(t, a, b)
}
