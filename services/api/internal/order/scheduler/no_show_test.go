package scheduler_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order/scheduler"
)

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO vendor (display_name,legal_name,contact_email,status) VALUES ('V','V Ltd','v-'||gen_random_uuid()||'@x.com','approved') RETURNING id`,
	).Scan(&id))
	return id
}

func seedEmployee(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO "user" (primary_email,display_name,role,status) VALUES ('e-'||gen_random_uuid()||'@x.com','E','employee','active') RETURNING id`,
	).Scan(&id))
	return id
}

func seedReadyOrder(t *testing.T, pool *pgxpool.Pool, vendorID, userID string, readyAt time.Time) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO "order" (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, ready_at, totp_secret)
		 VALUES ($1, $2, 'F12B-3F', $3, 'ready', 100, now(), $4, $5, decode('00','hex')) RETURNING id`,
		userID, vendorID,
		time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
		readyAt,
	).Scan(&id))
	return id
}

func TestNoShow_TransitionsReadyOlderThanMaxAge(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	vendorID := seedVendor(t, pool)
	userID := seedEmployee(t, pool)
	// Old order: ready_at = 14:00 (4h before the fixed clock at 18:00) → should transition
	oldOrder := seedReadyOrder(t, pool, vendorID, userID, time.Date(2026, 5, 13, 14, 0, 0, 0, time.UTC))
	// Recent order: ready_at = 17:30 (30 min before the fixed clock at 18:00) → should NOT transition
	recentOrder := seedReadyOrder(t, pool, vendorID, userID, time.Date(2026, 5, 13, 17, 30, 0, 0, time.UTC))

	// Build a Service using THIS pool with the same fixed clock as setup() (18:00 UTC).
	orderRepo := opg.NewOrderRepo(pool)
	svc := &order.Service{
		Pool:     pool,
		Orders:   orderRepo,
		OrdersTx: orderRepo,
		StateTx:  opg.NewStateEventRepo(pool),
		AuditTx:  opg.NewAuditRepo(pool),
		OutboxTx: opg.NewOutboxRepo(pool),
		Clock:    fixedClock{T: time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)},
	}
	sweep := &scheduler.NoShowSweep{
		Svc:    svc,
		MaxAge: 1 * time.Hour,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
	n, err := sweep.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "exactly 1 order should transition (the 4h-old one)")

	var oldStatus, recentStatus string
	pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, oldOrder).Scan(&oldStatus)
	pool.QueryRow(ctx, `SELECT status FROM "order" WHERE id=$1`, recentOrder).Scan(&recentStatus)
	assert.Equal(t, "no_show", oldStatus)
	assert.Equal(t, "ready", recentStatus)
}
