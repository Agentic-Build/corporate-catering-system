package settler_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	orderpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	payrollpgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll/settler"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/messaging"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

func setupPostgres(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/payroll/settler/settler_test.go → ../../../../../migrations
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())

	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

func setupNATS(t *testing.T) (string, func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	require.NoError(t, err)
	url, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	return url, func() { _ = c.Terminate(ctx) }
}

func setupMinIO(t *testing.T) (host, user, pass string, cleanup func()) {
	t.Helper()
	ctx := context.Background()
	c, err := tcminio.Run(ctx, "minio/minio:RELEASE.2024-01-16T16-07-38Z",
		tcminio.WithUsername("tbiteadmin"),
		tcminio.WithPassword("tbiteadmin"),
	)
	require.NoError(t, err)
	conn, err := c.ConnectionString(ctx)
	require.NoError(t, err)
	return "http://" + conn, c.Username, c.Password, func() { _ = c.Terminate(ctx) }
}

var userSeq atomic.Uint64

func seedUser(t *testing.T, pool *pgxpool.Pool, empID, displayName string) string {
	t.Helper()
	n := userSeq.Add(1)
	email := fmt.Sprintf("settler-user-%d@test.com", n)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, employee_id, plant, department, role)
VALUES ($1,$2,$3,$4,$5,'employee')
RETURNING id`,
		email, displayName, empID, "F12B-3F", "RD",
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestSettler_BatchLockedToCSVExport(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, pgCleanup := setupPostgres(t)
	defer pgCleanup()
	natsURL, natsCleanup := setupNATS(t)
	defer natsCleanup()
	minioEndpoint, accessKey, secretKey, minioCleanup := setupMinIO(t)
	defer minioCleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// --- NATS client + stream provisioning -----------------------------------
	nc, err := messaging.New(ctx, natsURL)
	require.NoError(t, err)
	defer nc.Close()
	require.NoError(t, nc.ProvisionStreams(ctx))

	// --- S3 client + bucket --------------------------------------------------
	s3c, err := storage.NewS3(ctx, storage.S3Config{
		Endpoint:        minioEndpoint,
		Region:          "us-east-1",
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Bucket:          "tbite",
		UsePathStyle:    true,
	})
	require.NoError(t, err)
	require.NoError(t, s3c.EnsureBucket(ctx))

	// --- Seed: batch (locked) + 2 entries ------------------------------------
	batchRepo := payrollpgrepo.NewBatchRepo(pool)
	entryRepo := payrollpgrepo.NewEntryRepo(pool)
	auditRepo := orderpgrepo.NewAuditRepo(pool)
	outboxRepo := orderpgrepo.NewOutboxRepo(pool)

	periodStart := time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2027, time.March, 31, 0, 0, 0, 0, time.UTC)
	batch := &payroll.Batch{PeriodStart: periodStart, PeriodEnd: periodEnd, Status: payroll.BatchStatusDraft}
	require.NoError(t, batchRepo.Create(ctx, batch))

	// Move directly to locked via UpdateStatusTx (simulating what Service.Lock
	// would do — but we want to skip emitting the outbox event here because
	// the test publishes the NATS message directly).
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return batchRepo.UpdateStatusTx(ctx, tx, batch.ID,
			payroll.BatchStatusDraft, payroll.BatchStatusLocked, nil)
	}))

	userA := seedUser(t, pool, "E001", "王小明")
	userB := seedUser(t, pool, "E002", "陳大華")

	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return entryRepo.CreateTx(ctx, tx, &payroll.Entry{
			BatchID: batch.ID, UserID: userA, OrderIDs: []string{}, AmountMinor: 12000, RefundedMinor: 0,
		})
	}))
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return entryRepo.CreateTx(ctx, tx, &payroll.Entry{
			BatchID: batch.ID, UserID: userB, OrderIDs: []string{}, AmountMinor: 18000, RefundedMinor: 2000,
		})
	}))

	// --- Settler ------------------------------------------------------------
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	s := &settler.Settler{
		JS:      nc.JS,
		Pool:    pool,
		Batches: batchRepo,
		Entries: entryRepo,
		Users:   settler.NewPgUserLookup(pool),
		Storage: s3c,
		Logger:  logger.With("component", "payroll-settler-test"),
		Audit:   auditRepo,
		Outbox:  outboxRepo,
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	runErr := make(chan error, 1)
	go func() { runErr <- s.Run(runCtx) }()

	// --- Publish payroll.batch_locked.v1 ------------------------------------
	payload, _ := json.Marshal(map[string]any{
		"batch_id":     batch.ID,
		"period_start": periodStart.Format("2006-01-02"),
		"period_end":   periodEnd.Format("2006-01-02"),
	})
	_, err = nc.JS.Publish(ctx, "payroll.batch_locked.v1", payload)
	require.NoError(t, err)

	// --- Assert: batch becomes exported within 10s --------------------------
	deadline := time.Now().Add(10 * time.Second)
	var exported *payroll.Batch
	for time.Now().Before(deadline) {
		b, err := batchRepo.GetByID(ctx, batch.ID)
		require.NoError(t, err)
		if b.Status == payroll.BatchStatusExported {
			exported = b
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, exported, "batch did not transition to exported in 10s")
	require.NotNil(t, exported.ExportURI)
	expectedURI := fmt.Sprintf("s3://tbite/payroll/%s.csv", batch.ID)
	assert.Equal(t, expectedURI, *exported.ExportURI)
	require.NotNil(t, exported.ExportedAt)

	// --- Assert: CSV exists in MinIO with BOM + header + 2 rows -------------
	key := fmt.Sprintf("payroll/%s.csv", batch.ID)
	body, err := s3c.GetObject(ctx, key)
	require.NoError(t, err)
	csvBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	_ = body.Close()

	// First 3 bytes must be UTF-8 BOM EF BB BF.
	require.GreaterOrEqual(t, len(csvBytes), 3)
	assert.Equal(t, []byte{0xEF, 0xBB, 0xBF}, csvBytes[:3], "expected UTF-8 BOM prefix")

	// Strip BOM and parse CSV.
	reader := csv.NewReader(bytes.NewReader(csvBytes[3:]))
	rows, err := reader.ReadAll()
	require.NoError(t, err)
	// 1 header + 2 entry rows = 3 total
	assert.Equal(t, 3, len(rows), "expected header + 2 entry rows")
	assert.Equal(t, []string{
		"employee_id", "primary_email", "display_name", "plant", "department",
		"amount_ntd", "refunded_ntd", "net_ntd", "batch_period", "exception",
	}, rows[0])
	// Verify a user_b row exists with net=16000
	var foundB bool
	for _, r := range rows[1:] {
		if r[0] == "E002" {
			assert.Equal(t, "陳大華", r[2])
			assert.Equal(t, "18000", r[5])
			assert.Equal(t, "2000", r[6])
			assert.Equal(t, "16000", r[7])
			assert.True(t, strings.Contains(r[8], "2027-03-01"))
			foundB = true
		}
	}
	assert.True(t, foundB, "expected entry row for user E002")

	// --- Assert: outbox row payroll.export_ready.v1 enqueued ----------------
	var exportReadyCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE subject='payroll.export_ready.v1' AND aggregate_id=$1`,
		batch.ID,
	).Scan(&exportReadyCount))
	assert.Equal(t, 1, exportReadyCount)

	// --- Replay the same event: settler must short-circuit ------------------
	_, err = nc.JS.Publish(ctx, "payroll.batch_locked.v1", payload)
	require.NoError(t, err)
	// Give settler a beat to process the replay.
	time.Sleep(1 * time.Second)
	var stillOne int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox_event WHERE subject='payroll.export_ready.v1' AND aggregate_id=$1`,
		batch.ID,
	).Scan(&stillOne))
	assert.Equal(t, 1, stillOne, "replayed event must not double-emit export_ready")

	// Cancel settler and wait for exit.
	runCancel()
	select {
	case err := <-runErr:
		if err != nil && err != context.Canceled {
			t.Logf("settler.Run returned: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("settler did not stop within 5s of ctx cancel")
	}
}
