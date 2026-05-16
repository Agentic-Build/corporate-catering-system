package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/postgres"
)

func TestOrderReader_GetOrderInfo(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	reader := fpg.NewOrderReader(pool)
	user := seedEmployeeUser(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrder(t, pool, user, vendor)

	o, err := reader.GetOrderInfo(ctx, orderID)
	require.NoError(t, err)
	assert.Equal(t, orderID, o.ID)
	assert.Equal(t, user, o.UserID)
	assert.Equal(t, vendor, o.VendorID)
	assert.Equal(t, "picked_up", o.Status)
}

func TestOrderReader_GetOrderInfo_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()

	reader := fpg.NewOrderReader(pool)
	_, err := reader.GetOrderInfo(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, feedback.ErrOrderNotFound)
}
