package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

func TestStateEventRepo_AppendAndList(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	orderRepo := pgrepo.NewOrderRepo(pool)
	eventRepo := pgrepo.NewStateEventRepo(pool)

	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	require.NoError(t, orderRepo.Create(ctx, o))

	role := "employee"
	from := order.StatusDraft
	ev := &order.StateEvent{
		OrderID:   o.ID,
		FromState: &from,
		ToState:   order.StatusPlaced,
		ActorID:   &uid,
		ActorRole: &role,
		Reason:    "user_place",
		Payload:   map[string]any{"total": 24000},
	}
	require.NoError(t, eventRepo.Append(ctx, ev))
	require.NotZero(t, ev.ID)
	require.False(t, ev.At.IsZero())

	// Append a second event without a FromState (initial draft creation marker)
	ev2 := &order.StateEvent{
		OrderID: o.ID,
		ToState: order.StatusCutoff,
		Reason:  "system_cutoff",
		Payload: map[string]any{},
	}
	require.NoError(t, eventRepo.Append(ctx, ev2))

	list, err := eventRepo.ListByOrder(ctx, o.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	// Ordered by at ASC
	assert.Equal(t, ev.ID, list[0].ID)
	require.NotNil(t, list[0].FromState)
	assert.Equal(t, order.StatusDraft, *list[0].FromState)
	assert.Equal(t, order.StatusPlaced, list[0].ToState)
	assert.Equal(t, "user_place", list[0].Reason)
	assert.Equal(t, float64(24000), list[0].Payload["total"])

	assert.Equal(t, ev2.ID, list[1].ID)
	assert.Nil(t, list[1].FromState)
	assert.Equal(t, order.StatusCutoff, list[1].ToState)
}

func TestStateEventRepo_AppendOnly_UpdateFails(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "employee")
	vid := seedApprovedVendor(t, pool)
	iid := seedActiveMenuItem(t, pool, vid, 12000)
	orderRepo := pgrepo.NewOrderRepo(pool)
	eventRepo := pgrepo.NewStateEventRepo(pool)

	day := time.Now().UTC().Truncate(24 * time.Hour)
	o := newOrder(t, uid, vid, iid, day)
	require.NoError(t, orderRepo.Create(ctx, o))

	from := order.StatusDraft
	ev := &order.StateEvent{
		OrderID: o.ID, FromState: &from, ToState: order.StatusPlaced,
		Reason: "x", Payload: map[string]any{},
	}
	require.NoError(t, eventRepo.Append(ctx, ev))

	// Direct UPDATE on the table should be rejected by the append-only trigger.
	_, err := pool.Exec(ctx, `UPDATE order_state_event SET reason='tampered' WHERE id=$1`, ev.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")

	// Direct DELETE should also be rejected.
	_, err = pool.Exec(ctx, `DELETE FROM order_state_event WHERE id=$1`, ev.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
}
