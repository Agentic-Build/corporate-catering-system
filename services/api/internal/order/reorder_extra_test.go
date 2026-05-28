package order_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
)

func TestReorder_SourceNotFound(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()

	_, _, userID := seedReorderScenario(t, env.Pool, []string{"A"})

	// A non-existent source order id surfaces the order repo's not-found error
	// (the GetByID error branch in Reorder).
	_, err := env.Reorder.Reorder(context.Background(), order.ReorderInput{
		UserID:        userID,
		SourceOrderID: "99999999-9999-9999-9999-999999999999",
		SupplyDate:    reorderTargetDate.Format("2006-01-02"),
		Plant:         reorderTestPlant,
	})
	assert.ErrorIs(t, err, order.ErrOrderNotFound)
}
