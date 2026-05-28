package leader_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/leader"
)

// When not running inside a K8s cluster, RunWithLease should invoke onLeading
// directly and propagate context cancellation as the returned error.
func TestRunWithLease_LocalFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	called := false
	err := leader.RunWithLease(ctx, leader.Config{LeaseName: "test"}, func(c context.Context) error {
		called = true
		<-c.Done()
		return c.Err()
	})
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, called, "onLeading must have been invoked in local fallback")
}
