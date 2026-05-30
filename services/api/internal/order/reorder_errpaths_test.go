package order_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	opg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	qpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota/postgres"
	vpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors/postgres"
)

// ---- reorder dep fakes (satisfy NewReorderService's deps) ----

// reorderItemGetter equivalent: GetByID returning a generic (non-not-found) error.
type errReorderItems struct {
	get func(ctx context.Context, id string) (*order.ReorderMenuItem, error)
}

func (e errReorderItems) GetByID(ctx context.Context, id string) (*order.ReorderMenuItem, error) {
	return e.get(ctx, id)
}

// reorderSupplyGetErr fails Get; DecrementTx is unused on this path.
type reorderSupplyGetErr struct {
	get func(ctx context.Context, id string, d time.Time) (*order.ReorderSupply, error)
	dec func(ctx context.Context, tx pgx.Tx, id string, d time.Time, n int) (int, error)
}

func (r reorderSupplyGetErr) Get(ctx context.Context, id string, d time.Time) (*order.ReorderSupply, error) {
	return r.get(ctx, id, d)
}

func (r reorderSupplyGetErr) DecrementTx(ctx context.Context, tx pgx.Tx, id string, d time.Time, n int) (int, error) {
	return r.dec(ctx, tx, id, d, n)
}

// errReorderVendors fails GetByID (drives buildReorderOrder's vendor lookup error).
type errReorderState struct{ order.StateEventAppender }

func (errReorderState) AppendTx(context.Context, pgx.Tx, *order.StateEvent) error { return errBoom }

type errReorderOutbox struct{ order.OutboxAppender }

func (errReorderOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return errBoom
}

type errReorderAudit struct{ order.AuditTxWriter }

func (errReorderAudit) WriteTx(context.Context, pgx.Tx, plaudit.Entry) error { return errBoom }

// newReorderWith builds a ReorderService over env.Pool with caller-supplied
// overrides applied to the default (real) deps.
func newReorderWith(t *testing.T, env reorderTestEnv, patch func(d *order.ReorderDeps)) *order.ReorderService {
	t.Helper()
	supplyRepo := qpg.NewSupplyRepo(env.Pool)
	d := order.ReorderDeps{
		Pool:    env.Pool,
		Orders:  opg.NewOrderRepo(env.Pool),
		Supply:  supplyRepoAdapter{inner: supplyRepo},
		Items:   itemRepoAdapter{inner: mpg.NewItemRepo(env.Pool)},
		Vendors: vpg.NewVendorRepo(env.Pool),
		Plants:  vpg.NewPlantMappingRepo(env.Pool),
		State:   opg.NewStateEventRepo(env.Pool),
		Audit:   opg.NewAuditRepo(env.Pool),
		Outbox:  opg.NewOutboxRepo(env.Pool),
		Clock:   fixedClock{T: reorderClockTime},
	}
	patch(&d)
	return order.NewReorderService(d)
}

// ---- error path tests ----

func TestReorder_PlantsLookupFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Plants = errPlants{d.Plants}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_ItemLookupGenericError(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Items = errReorderItems{get: func(context.Context, string) (*order.ReorderMenuItem, error) {
			return nil, errBoom
		}}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_ItemNotFoundTreatedAsArchived(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	// items.GetByID returns ErrReorderItemNotFound → classifyOne records it as
	// archived (no name), and with zero survivors no order is created.
	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Items = errReorderItems{get: func(context.Context, string) (*order.ReorderMenuItem, error) {
			return nil, order.ErrReorderItemNotFound
		}}
	})
	res, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	require.NoError(t, err)
	assert.Empty(t, res.NewOrderID)
	require.Len(t, res.UnavailableItems, 1)
	assert.Equal(t, "archived", res.UnavailableItems[0].Reason)
	assert.Equal(t, itemIDs[0], res.UnavailableItems[0].MenuItemID)
	assert.Empty(t, res.UnavailableItems[0].Name)
}

func TestReorder_SupplyGetGenericError(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Supply = reorderSupplyGetErr{
			get: func(context.Context, string, time.Time) (*order.ReorderSupply, error) {
				return nil, errBoom
			},
			dec: func(context.Context, pgx.Tx, string, time.Time, int) (int, error) { return 0, errBoom },
		}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_BuildOrderVendorLookupFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Vendors = errVendors{}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_DecrementTxFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	realSupply := supplyRepoAdapter{inner: qpg.NewSupplyRepo(env.Pool)}
	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Supply = reorderSupplyGetErr{
			get: realSupply.Get, // availability check passes
			dec: func(context.Context, pgx.Tx, string, time.Time, int) (int, error) {
				return 0, errBoom // tx decrement fails
			},
		}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_StateAppendFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.State = errReorderState{d.State}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_OutboxAppendFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Outbox = errReorderOutbox{d.Outbox}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestReorder_AuditWriteFails(t *testing.T) {
	env := setupReorder(t)
	defer env.Cleanup()
	_, itemIDs, userID := seedReorderScenario(t, env.Pool, []string{"A"})
	addSupply(t, env.Pool, itemIDs[0], reorderSourceDate, 5, 5, reorderSourceCutoff)
	addSupply(t, env.Pool, itemIDs[0], reorderTargetDate, 5, 5, reorderTargetCutoff)
	src := placeSourceOrder(t, env, userID, []order.PlaceItem{{MenuItemID: itemIDs[0], Qty: 1}})

	svc := newReorderWith(t, env, func(d *order.ReorderDeps) {
		d.Audit = errReorderAudit{d.Audit}
	})
	_, err := svc.Reorder(context.Background(), order.ReorderInput{
		UserID: userID, SourceOrderID: src.ID,
		SupplyDate: reorderTargetDate.Format("2006-01-02"), Plant: reorderTestPlant,
	})
	assert.ErrorIs(t, err, errBoom)
}
