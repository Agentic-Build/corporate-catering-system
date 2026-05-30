package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/oidc"
)

// TestGet_ConsumedKeyRedisError drives the branch where the live key is absent
// (redis.Nil) but reading the consumed key returns a real error.
func TestGet_ConsumedKeyRedisError(t *testing.T) {
	f := newFakeRESP(t)
	// First GET (live key) -> nil; second GET (consumed key) -> error.
	f.replySeq("GET", "$-1\r\n", "-ERR consumed boom\r\n")
	rdb := newFakeRedisClient(t, f)
	s := oidc.NewRedisStateStore(rdb, time.Minute)

	_, err := s.Get(context.Background(), "st")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis get consumed state")
}

// TestConsume_ExecError drives the pipeline EXEC failure branch: the live key
// GET returns a value, then the transactional pipeline EXEC errors.
func TestConsume_ExecError(t *testing.T) {
	f := newFakeRESP(t)
	// GET (live key) returns a bulk-string value so we proceed to the pipeline.
	f.reply("GET", "$2\r\n{}\r\n")
	// Inside MULTI, queued commands reply +QUEUED; EXEC fails.
	f.reply("MULTI", "+OK\r\n")
	f.reply("SET", "+QUEUED\r\n")
	f.reply("DEL", "+QUEUED\r\n")
	f.reply("EXEC", "-ERR exec boom\r\n")
	rdb := newFakeRedisClient(t, f)
	s := oidc.NewRedisStateStore(rdb, time.Minute)

	err := s.Consume(context.Background(), "st")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis consume state")
}
