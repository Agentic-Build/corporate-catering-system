package order_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
)

func TestStateMachine_HappyPath(t *testing.T) {
	assert.True(t, order.CanTransition(order.StatusDraft, order.StatusPlaced))
	assert.True(t, order.CanTransition(order.StatusPlaced, order.StatusCutoff))
	assert.True(t, order.CanTransition(order.StatusCutoff, order.StatusReady))
	assert.True(t, order.CanTransition(order.StatusReady, order.StatusPickedUp))
	assert.True(t, order.CanTransition(order.StatusPickedUp, order.StatusRefunded))
}

func TestStateMachine_Cancel(t *testing.T) {
	assert.True(t, order.CanTransition(order.StatusDraft, order.StatusCancelled))
	assert.True(t, order.CanTransition(order.StatusPlaced, order.StatusCancelled))
	assert.True(t, order.CanTransition(order.StatusCutoff, order.StatusCancelled))
}

func TestStateMachine_Forbidden(t *testing.T) {
	assert.False(t, order.CanTransition(order.StatusPlaced, order.StatusDraft))
	assert.False(t, order.CanTransition(order.StatusCancelled, order.StatusPlaced))
	assert.False(t, order.CanTransition(order.StatusCutoff, order.StatusPlaced))
	assert.False(t, order.CanTransition(order.StatusRefunded, order.StatusPickedUp))
	assert.False(t, order.CanTransition(order.StatusCancelled, order.StatusRefunded))
}

func TestStateMachine_TerminalStates(t *testing.T) {
	// cancelled and refunded are terminal
	for _, s := range []order.Status{order.StatusCancelled, order.StatusRefunded} {
		for _, t2 := range []order.Status{order.StatusDraft, order.StatusPlaced, order.StatusCutoff, order.StatusReady} {
			assert.False(t, order.CanTransition(s, t2), "expected %s → %s forbidden", s, t2)
		}
	}
}
