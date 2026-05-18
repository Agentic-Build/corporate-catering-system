package compliance_test

import (
	"context"
	"path/filepath"
	"runtime"
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
	cpg "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
	vendorpkg "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// fakeSuspender records vendor ids it was asked to suspend and can be set to
// return an error (e.g. simulating an already-suspended vendor).
type fakeSuspender struct {
	suspended []string
	err       error
}

func (f *fakeSuspender) Suspend(_ context.Context, vendorID string) error {
	if f.err != nil {
		return f.err
	}
	f.suspended = append(f.suspended, vendorID)
	return nil
}

func setupGov(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("tbite"),
		tcpostgres.WithUsername("tbite"),
		tcpostgres.WithPassword("tbite"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	_, thisFile, _, _ := runtime.Caller(0)
	migrationsDir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
	m, err := migrate.New("file://"+migrationsDir, dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	return pool, func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
}

// seedAnomalyForVendor inserts an approved vendor and an open anomaly targeting
// it, returning (vendorID, anomalyID).
func seedAnomalyForVendor(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var vendorID string
	require.NoError(t, pool.QueryRow(ctx, `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ('Gov Vendor', 'Gov Ltd', 'gov@test.com', 'approved')
RETURNING id`).Scan(&vendorID))

	anomaly := &compliance.Anomaly{
		Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityHigh, Status: compliance.AnomalyOpen,
	}
	require.NoError(t, cpg.NewAnomalyRepo(pool).Open(ctx, anomaly))
	return vendorID, anomaly.ID
}

// seedAdmin inserts a welfare_admin user and returns its UUID — anomaly
// triage stamps triaged_by, a UUID FK to "user".
func seedAdmin(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ('gov-admin@test.com', 'Gov Admin', 'welfare_admin')
RETURNING id`).Scan(&id))
	return id
}

func newGovService(pool *pgxpool.Pool, gov compliance.VendorSuspender) *compliance.Service {
	return &compliance.Service{
		Pool:      pool,
		Anomaly:   cpg.NewAnomalyRepo(pool),
		Audit:     opg.NewAuditRepo(pool),
		VendorGov: gov,
	}
}

func TestTriageAnomaly_InvalidAction(t *testing.T) {
	pool, cleanup := setupGov(t)
	defer cleanup()
	_, anomalyID := seedAnomalyForVendor(t, pool)
	admin := seedAdmin(t, pool)
	svc := newGovService(pool, &fakeSuspender{})

	err := svc.TriageAnomaly(context.Background(), anomalyID, admin, "looking", "demote")
	assert.ErrorIs(t, err, compliance.ErrInvalidAction)
}

func TestTriageAnomaly_WarnWritesVendorWarningAudit(t *testing.T) {
	pool, cleanup := setupGov(t)
	defer cleanup()
	ctx := context.Background()
	vendorID, anomalyID := seedAnomalyForVendor(t, pool)
	admin := seedAdmin(t, pool)
	sus := &fakeSuspender{}
	svc := newGovService(pool, sus)

	require.NoError(t, svc.TriageAnomaly(ctx, anomalyID, admin, "first warning", compliance.ActionWarn))

	// Anomaly is triaged.
	a, err := svc.GetAnomaly(ctx, anomalyID)
	require.NoError(t, err)
	assert.Equal(t, compliance.AnomalyTriaged, a.Status)

	// A vendor.warning audit row was written against the target vendor.
	var warnCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE action='vendor.warning' AND target_id=$1`,
		vendorID).Scan(&warnCount))
	assert.Equal(t, 1, warnCount)
	assert.Empty(t, sus.suspended, "warn must not suspend")
}

func TestTriageAnomaly_SuspendCallsVendorGov(t *testing.T) {
	pool, cleanup := setupGov(t)
	defer cleanup()
	ctx := context.Background()
	vendorID, anomalyID := seedAnomalyForVendor(t, pool)
	admin := seedAdmin(t, pool)
	sus := &fakeSuspender{}
	svc := newGovService(pool, sus)

	require.NoError(t, svc.TriageAnomaly(ctx, anomalyID, admin, "repeated lateness", compliance.ActionSuspend))
	assert.Equal(t, []string{vendorID}, sus.suspended)
}

func TestTriageAnomaly_SuspendToleratesAlreadySuspended(t *testing.T) {
	pool, cleanup := setupGov(t)
	defer cleanup()
	ctx := context.Background()
	_, anomalyID := seedAnomalyForVendor(t, pool)
	admin := seedAdmin(t, pool)
	// Suspender reports the vendor is not in an approved state — triage must
	// still succeed, since the governance goal is already met.
	svc := newGovService(pool, &fakeSuspender{err: vendorpkg.ErrInvalidStatus})

	require.NoError(t, svc.TriageAnomaly(ctx, anomalyID, admin, "", compliance.ActionSuspend))
}
