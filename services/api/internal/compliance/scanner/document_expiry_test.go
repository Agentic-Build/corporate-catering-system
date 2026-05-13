package scanner_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	cpgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance/scanner"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
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
	// services/api/internal/compliance/scanner/document_expiry_test.go
	//   → ../../../../../migrations
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

var vendorSeedCounter atomic.Uint64

func seedApprovedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorSeedCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved')
RETURNING id`,
		fmt.Sprintf("scanner-vendor-%d", n),
		fmt.Sprintf("scanner-vendor-%d Ltd", n),
		fmt.Sprintf("scanner-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestDocumentExpiryScanner_RunOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	docRepo := cpgrepo.NewDocumentRepo(pool)
	anomRepo := cpgrepo.NewAnomalyRepo(pool)
	vendorID := seedApprovedVendor(t, pool)

	// Fix the scanner's clock to "today" so the day-bucket math is
	// deterministic regardless of when the test runs.
	now := time.Date(2027, time.June, 15, 12, 0, 0, 0, time.UTC)
	exp12 := now.AddDate(0, 0, 12) // medium
	exp5 := now.AddDate(0, 0, 5)   // high
	expPast := now.AddDate(0, 0, -1)

	doc1 := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://doc1", Filename: "license.pdf", ExpiresAt: &exp12,
	}
	doc2 := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindFoodSafetyPermit,
		BlobURI: "s3://doc2", Filename: "permit.pdf", ExpiresAt: &exp5,
	}
	doc3 := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindInsurance,
		BlobURI: "s3://doc3", Filename: "insurance.pdf", ExpiresAt: &expPast,
	}
	for _, d := range []*compliance.Document{doc1, doc2, doc3} {
		require.NoError(t, docRepo.Create(ctx, d))
		require.NoError(t, docRepo.UpdateStatus(ctx, d.ID, compliance.DocStatusApproved, nil, ""))
	}

	s := &scanner.DocumentExpiryScanner{
		Pool:       pool,
		Docs:       docRepo,
		Anomaly:    anomRepo,
		DaysWindow: 14,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:      clock.FixedClock{T: now},
	}

	handled, err := s.RunOnce(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, handled)

	// doc3 must now be status='expired'.
	got3, err := docRepo.GetByID(ctx, doc3.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.DocStatusExpired, got3.Status)

	// Severity assertions: doc1 medium (12d), doc2 high (5d), doc3 critical (past).
	type want struct {
		kind     string
		severity compliance.AnomalySeverity
	}
	expected := map[string]want{
		doc1.ID: {"document_expiring", compliance.SeverityMedium},
		doc2.ID: {"document_expiring", compliance.SeverityHigh},
		doc3.ID: {"document_expired", compliance.SeverityCritical},
	}

	anomalies, err := anomRepo.List(ctx,
		[]compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	require.Len(t, anomalies, 3)

	got := map[string]want{}
	for _, a := range anomalies {
		assert.Equal(t, "vendor_document", a.TargetKind)
		got[a.TargetID] = want{kind: a.Kind, severity: a.Severity}
	}
	assert.Equal(t, expected, got)

	// Idempotency: a second scan must not create more anomalies.
	// doc3 is now 'expired' so it drops out of ListPastExpiry; doc1+doc2
	// still match ListExpiringBefore and their existing 'open' anomalies
	// get UPDATEd, not duplicated.
	_, err = s.RunOnce(ctx)
	require.NoError(t, err)
	anomalies2, err := anomRepo.List(ctx,
		[]compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	assert.Len(t, anomalies2, 3, "scanner should be idempotent")
}
