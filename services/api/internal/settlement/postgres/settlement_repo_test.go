package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
	spg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement/postgres"
)

func aprilPeriod() (time.Time, time.Time) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, -1)
	return start, end
}

func newClosed(vendorID, closedBy string, start, end time.Time, orderIDs []string) *settlement.Settlement {
	return &settlement.Settlement{
		VendorID:     vendorID,
		PeriodStart:  start,
		PeriodEnd:    end,
		OrderCount:   len(orderIDs),
		PortionCount: len(orderIDs),
		GrossMinor:   int64(len(orderIDs)) * 10000,
		OrderIDs:     orderIDs,
		Status:       settlement.StatusClosed,
		ClosedBy:     &closedBy,
	}
}

func TestSettlementRepo_CreateAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	o1 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 2)
	o2 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), "no_show", 8000, 1)

	st := newClosed(vendor, admin, start, end, []string{o1, o2})
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, st) })
	require.NoError(t, err)
	require.NotEmpty(t, st.ID)

	got, err := repo.GetByID(ctx, st.ID)
	require.NoError(t, err)
	assert.Equal(t, vendor, got.VendorID)
	assert.Equal(t, settlement.StatusClosed, got.Status)
	assert.ElementsMatch(t, []string{o1, o2}, got.OrderIDs)
}

func TestSettlementRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := spg.NewSettlementRepo(pool)

	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, settlement.ErrSettlementNotFound)
}

// The partial unique index forbids two active (closed) settlements for the same
// vendor+period; voiding the first must free the period for re-close.
func TestSettlementRepo_ActiveUniqueIndex(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	first := newClosed(vendor, admin, start, end, []string{})
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, first) }))

	// Second active row for the same vendor+period must be rejected.
	dup := newClosed(vendor, admin, start, end, []string{})
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, dup) })
	assert.ErrorIs(t, err, settlement.ErrPeriodAlreadyClosed)

	// Void the first → period is free again.
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.VoidTx(ctx, tx, first.ID) }))
	reclosed := newClosed(vendor, admin, start, end, []string{})
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, reclosed) }))

	got, err := repo.GetByID(ctx, first.ID)
	require.NoError(t, err)
	assert.Equal(t, settlement.StatusVoid, got.Status)
}

func TestSettlementRepo_VoidTx_RejectsNonClosed(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)
	st := newClosed(vendor, admin, start, end, []string{})
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, st) }))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.VoidTx(ctx, tx, st.ID) }))

	// Voiding an already-void row is an invalid transition.
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.VoidTx(ctx, tx, st.ID) })
	assert.ErrorIs(t, err, settlement.ErrInvalidTransition)
}

func TestSettlementRepo_ListByVendor(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	apr := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	may := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	stA1 := newClosed(vendorA, admin, apr, apr.AddDate(0, 1, -1), []string{})
	stA2 := newClosed(vendorA, admin, may, may.AddDate(0, 1, -1), []string{})
	stB1 := newClosed(vendorB, admin, apr, apr.AddDate(0, 1, -1), []string{})
	for _, s := range []*settlement.Settlement{stA1, stA2, stB1} {
		s := s
		require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return repo.CreateTx(ctx, tx, s) }))
	}

	got, err := repo.ListByVendor(ctx, vendorA)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Newest period first.
	assert.True(t, got[0].PeriodStart.After(got[1].PeriodStart))
}

func TestSettlementRepo_ListByPeriod(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	apr := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	aprEnd := apr.AddDate(0, 1, -1)
	may := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, newClosed(vendorA, admin, apr, aprEnd, []string{}))
	}))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, newClosed(vendorB, admin, apr, aprEnd, []string{}))
	}))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, newClosed(vendorA, admin, may, may.AddDate(0, 1, -1), []string{}))
	}))

	got, err := repo.ListByPeriod(ctx, apr, aprEnd)
	require.NoError(t, err)
	assert.Len(t, got, 2) // both vendors' April settlements, not May
}

// AggregateByVendor must include only picked_up/no_show, slice by supply_date,
// and roll order_item.qty into portion_count — mirroring payroll inclusion.
func TestSettlementRepo_AggregateByVendor(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendorA := seedVendor(t, pool)
	vendorB := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)

	// Vendor A: 1 picked_up (qty 3) + 1 no_show (qty 2) in April → counted.
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 2), "picked_up", 12000, 3)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 5), "no_show", 8000, 2)
	// Vendor A: cancelled + refunded + placed in April → excluded.
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 6), "cancelled", 9999, 1)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 7), "refunded", 9999, 1)
	seedOrder(t, pool, user, vendorA, start.AddDate(0, 0, 8), "placed", 9999, 1)
	// Vendor A: picked_up but May (out of period) → excluded.
	seedOrder(t, pool, user, vendorA, end.AddDate(0, 0, 1), "picked_up", 99999, 9)
	// Vendor B: 1 picked_up (qty 1) in April.
	seedOrder(t, pool, user, vendorB, start.AddDate(0, 0, 3), "picked_up", 5000, 1)

	aggs, err := repo.AggregateByVendor(ctx, start, end)
	require.NoError(t, err)
	require.Len(t, aggs, 2)

	byVendor := map[string]*settlement.VendorAggregate{}
	for _, a := range aggs {
		byVendor[a.VendorID] = a
	}
	a := byVendor[vendorA]
	require.NotNil(t, a)
	assert.Equal(t, 2, a.OrderCount)
	assert.Equal(t, 5, a.PortionCount)          // 3 + 2
	assert.Equal(t, int64(20000), a.GrossMinor) // 12000 + 8000
	assert.Len(t, a.OrderIDs, 2)

	b := byVendor[vendorB]
	require.NotNil(t, b)
	assert.Equal(t, 1, b.OrderCount)
	assert.Equal(t, 1, b.PortionCount)
	assert.Equal(t, int64(5000), b.GrossMinor)
}

func TestSettlementRepo_AggregateForVendor_Empty(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)

	a, err := repo.AggregateForVendor(ctx, vendor, start, end)
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.Equal(t, 0, a.OrderCount)
	assert.Equal(t, int64(0), a.GrossMinor)
	assert.Empty(t, a.OrderIDs)
}

func TestSettlementRepo_StatusBreakdownForVendor(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)

	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 1), "picked_up", 1000, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 1000, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 3), "no_show", 1000, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 4), "cancelled", 1000, 1)
	seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 5), "refunded", 1000, 1)

	b, err := repo.StatusBreakdownForVendor(ctx, vendor, start, end)
	require.NoError(t, err)
	assert.Equal(t, 2, b.PickedUp)
	assert.Equal(t, 1, b.NoShow)
	assert.Equal(t, 1, b.Cancelled)
	assert.Equal(t, 1, b.Refunded)
}

func TestSettlementRepo_OrderLinesByIDs(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := spg.NewSettlementRepo(pool)

	start, _ := aprilPeriod()
	vendor := seedVendor(t, pool)
	user := seedEmployeeUser(t, pool)
	o1 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 3)
	o2 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 5), "no_show", 8000, 2)

	lines, err := repo.OrderLinesByIDs(ctx, []string{o1, o2})
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, o1, lines[0].OrderID)
	assert.Equal(t, int64(12000), lines[0].TotalPriceMinor)
	assert.Equal(t, 3, lines[0].PortionCount)

	// Empty input returns an empty (non-nil) slice.
	empty, err := repo.OrderLinesByIDs(ctx, nil)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)
}

func TestSettlementRepo_CreateTx_RequiresTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := spg.NewSettlementRepo(pool)
	err := repo.CreateTx(context.Background(), nil, &settlement.Settlement{})
	assert.Error(t, err)
	assert.False(t, errors.Is(err, pgx.ErrNoRows))
}
