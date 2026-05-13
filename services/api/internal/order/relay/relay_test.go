package relay_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order/relay"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/messaging"
)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
}

func setupPgAndMigrate(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, _ := c.ConnectionString(ctx, "sslmode=disable")
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	return pool, func() { pool.Close(); _ = c.Terminate(ctx) }
}

func setupNATS(t *testing.T) (*messaging.Client, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err)
	url, _ := c.ConnectionString(ctx)
	cl, err := messaging.New(ctx, url)
	require.NoError(t, err)
	require.NoError(t, cl.ProvisionStreams(ctx))
	return cl, func() { cl.Close(); _ = c.Terminate(ctx) }
}

func TestRelay_CycleDrainsAndPublishes(t *testing.T) {
	pool, cleanupPG := setupPgAndMigrate(t)
	defer cleanupPG()
	nats, cleanupNATS := setupNATS(t)
	defer cleanupNATS()
	ctx := context.Background()

	outbox := postgres.NewOutboxRepo(pool)

	// Seed 3 events directly via SQL
	_, err := pool.Exec(ctx, `
INSERT INTO outbox_event (aggregate_type, aggregate_id, subject, payload, headers)
VALUES
  ('order', gen_random_uuid(), 'order.placed.v1', '{"order_id":"a"}'::jsonb, '{}'::jsonb),
  ('order', gen_random_uuid(), 'order.placed.v1', '{"order_id":"b"}'::jsonb, '{}'::jsonb),
  ('order', gen_random_uuid(), 'order.cutoff.v1', '{"order_id":"c"}'::jsonb, '{}'::jsonb)`)
	require.NoError(t, err)

	r := &relay.Relay{
		Outbox: outbox,
		JS:     nats.JS,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		Batch:  10,
		Sleep:  10 * time.Millisecond,
	}
	// call cycle directly to assert progress
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		_ = r.Run(runCtx)
	}()

	// Eventually 3 messages reach the stream
	stream, err := nats.JS.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	deadline := time.Now().Add(5 * time.Second)
	var msgs uint64
	for time.Now().Before(deadline) {
		info, _ := stream.Info(ctx)
		if info != nil {
			msgs = info.State.Msgs
			if msgs >= 3 {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, msgs, uint64(3), "expected 3 messages on ORDERS_V1")

	// All rows should be marked published
	var unpublished int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM outbox_event WHERE published_at IS NULL`).Scan(&unpublished))
	assert.Equal(t, 0, unpublished)
}

func TestRelay_CycleEmptyOutbox(t *testing.T) {
	pool, cleanupPG := setupPgAndMigrate(t)
	defer cleanupPG()
	nats, cleanupNATS := setupNATS(t)
	defer cleanupNATS()
	outbox := postgres.NewOutboxRepo(pool)
	r := &relay.Relay{Outbox: outbox, JS: nats.JS, Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)), Batch: 100}

	runCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	err := r.Run(runCtx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// pull receives all available messages on subject pattern.
func pullAll(ctx context.Context, t *testing.T, js jetstream.JetStream, subject string, max int) []jetstream.Msg {
	t.Helper()
	stream, err := js.Stream(ctx, "ORDERS_V1")
	require.NoError(t, err)
	c, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{
		Name:          "pull-" + t.Name(),
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subject,
	})
	require.NoError(t, err)
	batch, err := c.FetchNoWait(max)
	require.NoError(t, err)
	var out []jetstream.Msg
	for m := range batch.Messages() {
		out = append(out, m)
		_ = m.Ack()
	}
	return out
}
