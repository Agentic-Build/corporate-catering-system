package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/postgres"
)

// makeEntry seeds a batch + entry pair for dispute tests. The entry references
// a real picked_up order so the FK to "order" is satisfied.
func makeEntry(t *testing.T, pool *pgxpool.Pool, month time.Month) (entryID, userID, orderID string) {
	t.Helper()
	ctx := context.Background()
	batchRepo := pgrepo.NewBatchRepo(pool)
	entryRepo := pgrepo.NewEntryRepo(pool)

	start := time.Date(2028, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	batch := &payroll.Batch{PeriodStart: start, PeriodEnd: end, Status: payroll.BatchStatusDraft}
	require.NoError(t, batchRepo.Create(ctx, batch))

	userID = seedEmployeeUser(t, pool)
	vendor := seedApprovedVendor(t, pool)
	orderID = seedOrder(t, pool, userID, vendor)

	entry := &payroll.Entry{
		BatchID:     batch.ID,
		UserID:      userID,
		OrderIDs:    []string{orderID},
		AmountMinor: 12000,
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return entryRepo.CreateTx(ctx, tx, entry)
	}))
	return entry.ID, userID, orderID
}

func TestDisputeRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDisputeRepo(pool)

	entryID, userID, orderID := makeEntry(t, pool, time.January)

	d := &payroll.Dispute{
		EntryID:  entryID,
		OrderID:  orderID,
		OpenedBy: userID,
		Reason:   "missing item",
	}
	require.NoError(t, repo.Create(ctx, d))
	require.NotEmpty(t, d.ID)
	assert.Equal(t, payroll.DisputeStatusOpen, d.Status)

	got, err := repo.GetByID(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, entryID, got.EntryID)
	assert.Equal(t, orderID, got.OrderID)
	assert.Equal(t, userID, got.OpenedBy)
	assert.Equal(t, "missing item", got.Reason)
	assert.Equal(t, payroll.DisputeStatusOpen, got.Status)
	assert.Equal(t, int64(0), got.RefundMinor)
	assert.Nil(t, got.ResolvedBy)
	assert.Nil(t, got.ResolvedAt)
}

func TestDisputeRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewDisputeRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, payroll.ErrDisputeNotFound)
}

func TestDisputeRepo_UpdateStatus_Resolve(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDisputeRepo(pool)
	admin := seedAdminUser(t, pool)
	entryID, userID, orderID := makeEntry(t, pool, time.February)

	d := &payroll.Dispute{
		EntryID:  entryID,
		OrderID:  orderID,
		OpenedBy: userID,
		Reason:   "wrong food",
	}
	require.NoError(t, repo.Create(ctx, d))

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, d.ID, payroll.DisputeStatusResolvedRefund, &admin, "refund full", 12000)
	}))
	got, err := repo.GetByID(ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, payroll.DisputeStatusResolvedRefund, got.Status)
	require.NotNil(t, got.ResolvedBy)
	assert.Equal(t, admin, *got.ResolvedBy)
	require.NotNil(t, got.ResolvedAt)
	assert.Equal(t, "refund full", got.Resolution)
	assert.Equal(t, int64(12000), got.RefundMinor)
}

func TestDisputeRepo_ListByStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDisputeRepo(pool)
	admin := seedAdminUser(t, pool)

	// Create 3 disputes; resolve one as refund, one as reject, leave one open.
	var ids [3]string
	for i := 0; i < 3; i++ {
		entryID, userID, orderID := makeEntry(t, pool, time.Month(time.March+time.Month(i)))
		d := &payroll.Dispute{EntryID: entryID, OrderID: orderID, OpenedBy: userID, Reason: "r"}
		require.NoError(t, repo.Create(ctx, d))
		ids[i] = d.ID
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, ids[1], payroll.DisputeStatusResolvedRefund, &admin, "ok", 5000)
	}))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.UpdateStatusTx(ctx, tx, ids[2], payroll.DisputeStatusResolvedReject, &admin, "no proof", 0)
	}))

	open, err := repo.ListByStatus(ctx, []payroll.DisputeStatus{payroll.DisputeStatusOpen})
	require.NoError(t, err)
	require.Len(t, open, 1)
	assert.Equal(t, ids[0], open[0].ID)

	refunded, err := repo.ListByStatus(ctx, []payroll.DisputeStatus{payroll.DisputeStatusResolvedRefund})
	require.NoError(t, err)
	require.Len(t, refunded, 1)
	assert.Equal(t, ids[1], refunded[0].ID)

	// No filter → all 3
	all, err := repo.ListByStatus(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestDisputeRepo_ListByUser(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewDisputeRepo(pool)

	// user A opens 2 disputes; user B opens 1.
	entryA1, userA, orderA1 := makeEntry(t, pool, time.June)
	dA1 := &payroll.Dispute{EntryID: entryA1, OrderID: orderA1, OpenedBy: userA, Reason: "a1"}
	require.NoError(t, repo.Create(ctx, dA1))

	entryA2, _, orderA2 := makeEntry(t, pool, time.July)
	dA2 := &payroll.Dispute{EntryID: entryA2, OrderID: orderA2, OpenedBy: userA, Reason: "a2"}
	require.NoError(t, repo.Create(ctx, dA2))

	entryB, userB, orderB := makeEntry(t, pool, time.August)
	dB := &payroll.Dispute{EntryID: entryB, OrderID: orderB, OpenedBy: userB, Reason: "b"}
	require.NoError(t, repo.Create(ctx, dB))

	listA, err := repo.ListByUser(ctx, userA)
	require.NoError(t, err)
	require.Len(t, listA, 2)
	for _, d := range listA {
		assert.Equal(t, userA, d.OpenedBy)
	}

	listB, err := repo.ListByUser(ctx, userB)
	require.NoError(t, err)
	require.Len(t, listB, 1)
	assert.Equal(t, dB.ID, listB[0].ID)
}
