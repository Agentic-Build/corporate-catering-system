package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
	spg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement/postgres"
)

// cancelledCtx returns a context that is already cancelled, so any pool/tx
// query made with it fails immediately. This drives the generic DB-error
// branches (return fmt.Errorf("..."), return nil, err) that the happy-path
// tests never reach.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestSettlementRepo_ErrorPaths(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := spg.NewSettlementRepo(pool)
	cctx := cancelledCtx()
	start, end := aprilPeriod()
	missing := "00000000-0000-0000-0000-000000000000"

	// CreateTx generic error: real tx, cancelled ctx → QueryRow/Scan fails.
	// The error string must NOT match vendor_settlement_active_idx, so it falls
	// through to the wrapped "create settlement" branch.
	err := pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.CreateTx(cctx, tx, &settlement.Settlement{
			VendorID:    missing,
			PeriodStart: start,
			PeriodEnd:   end,
			OrderIDs:    []string{},
		})
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create settlement")
	assert.NotErrorIs(t, err, settlement.ErrPeriodAlreadyClosed)

	// GetByID generic scan error (cancelled ctx → not ErrNoRows).
	_, err = repo.GetByID(cctx, missing)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan settlement")
	assert.NotErrorIs(t, err, settlement.ErrSettlementNotFound)

	// ListByVendor Query error.
	_, err = repo.ListByVendor(cctx, missing)
	require.Error(t, err)

	// ListByPeriod Query error.
	_, err = repo.ListByPeriod(cctx, start, end)
	require.Error(t, err)

	// VoidTx Exec error.
	err = pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.VoidTx(cctx, tx, missing)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "void settlement")

	// AggregateByVendor Query error.
	_, err = repo.AggregateByVendor(cctx, start, end)
	require.Error(t, err)

	// AggregateForVendor generic error.
	_, err = repo.AggregateForVendor(cctx, missing, start, end)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "aggregate for vendor")

	// StatusBreakdownForVendor error.
	_, err = repo.StatusBreakdownForVendor(cctx, missing, start, end)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status breakdown")

	// OrderLinesByIDs Query error (non-empty input so it reaches the query).
	_, err = repo.OrderLinesByIDs(cctx, []string{missing})
	require.Error(t, err)
}

// TestSettlementRepo_VoidTx_RequiresTx covers the nil-tx guard in VoidTx, the
// twin of the already-covered CreateTx nil-tx guard.
func TestSettlementRepo_VoidTx_RequiresTx(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := spg.NewSettlementRepo(pool)
	err := repo.VoidTx(context.Background(), nil, "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires a tx")
}

// TestSettlementRepo_RowScanErrors drives the per-row Scan error branches inside
// the streaming collectors. A context cancelled AFTER Query returns but before
// the rows are drained makes rows.Scan / rows.Err surface an error mid-iteration,
// exercising the "if err := rows.Scan(...); err != nil { return nil, err }" and
// "return ..., rows.Err()" failure arms that the happy paths never hit.
func TestSettlementRepo_RowScanErrors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := spg.NewSettlementRepo(pool)

	start, end := aprilPeriod()
	vendor := seedVendor(t, pool)
	admin := seedAdminUser(t, pool)
	user := seedEmployeeUser(t, pool)
	o1 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 2), "picked_up", 12000, 3)
	o2 := seedOrder(t, pool, user, vendor, start.AddDate(0, 0, 5), "no_show", 8000, 2)
	st := newClosed(vendor, admin, start, end, []string{o1, o2})
	require.NoError(t, pgx.BeginFunc(context.Background(), pool, func(tx pgx.Tx) error {
		return repo.CreateTx(context.Background(), tx, st)
	}))

	// Cancel mid-flight: start a request whose ctx we cancel right away so the
	// row stream errors while being read rather than at Query time.
	for _, tc := range []struct {
		name string
		run  func(ctx context.Context) error
	}{
		{"ListByVendor", func(ctx context.Context) error { _, e := repo.ListByVendor(ctx, vendor); return e }},
		{"ListByPeriod", func(ctx context.Context) error { _, e := repo.ListByPeriod(ctx, start, end); return e }},
		{"AggregateByVendor", func(ctx context.Context) error { _, e := repo.AggregateByVendor(ctx, start, end); return e }},
		{"OrderLinesByIDs", func(ctx context.Context) error { _, e := repo.OrderLinesByIDs(ctx, []string{o1, o2}); return e }},
	} {
		tc := tc
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := tc.run(ctx)
		require.Error(t, err, tc.name)
	}
}
