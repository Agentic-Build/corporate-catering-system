package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	pgrepo "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/postgres"
)

func TestAnomalyRepo_OpenAndGet(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	a := &compliance.Anomaly{
		Kind:        "on_time_rate_drop",
		TargetKind:  "vendor",
		TargetID:    vendorID,
		Severity:    compliance.SeverityHigh,
		Payload:     map[string]any{"rate": 0.72, "window": "7d"},
		EvidenceURI: []string{"s3://reports/r1", "s3://reports/r2"},
	}
	require.NoError(t, repo.Open(ctx, a))
	require.NotEmpty(t, a.ID)

	got, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, "on_time_rate_drop", got.Kind)
	assert.Equal(t, "vendor", got.TargetKind)
	assert.Equal(t, vendorID, got.TargetID)
	assert.Equal(t, compliance.SeverityHigh, got.Severity)
	assert.Equal(t, compliance.AnomalyOpen, got.Status)
	assert.Equal(t, []string{"s3://reports/r1", "s3://reports/r2"}, got.EvidenceURI)
	require.NotNil(t, got.Payload)
	assert.Equal(t, "7d", got.Payload["window"])
}

func TestAnomalyRepo_GetByID_NotFound(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewAnomalyRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, compliance.ErrAnomalyNotFound)
}

func TestAnomalyRepo_Open_Dedup(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	a1 := &compliance.Anomaly{
		Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityMedium,
		Payload:  map[string]any{"rate": 0.80},
	}
	require.NoError(t, repo.Open(ctx, a1))
	firstID := a1.ID
	firstCreated := a1.CreatedAt

	// Re-opening with same (kind,target_kind,target_id) must update, not insert.
	a2 := &compliance.Anomaly{
		Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
		Severity:    compliance.SeverityCritical,
		Payload:     map[string]any{"rate": 0.55},
		EvidenceURI: []string{"s3://reports/new"},
	}
	require.NoError(t, repo.Open(ctx, a2))
	assert.Equal(t, firstID, a2.ID, "must reuse the same anomaly row")

	got, err := repo.GetByID(ctx, firstID)
	require.NoError(t, err)
	assert.Equal(t, compliance.SeverityCritical, got.Severity)
	assert.Equal(t, []string{"s3://reports/new"}, got.EvidenceURI)
	require.NotNil(t, got.Payload)
	assert.InDelta(t, 0.55, got.Payload["rate"], 0.0001)
	// created_at preserved across upserts.
	assert.True(t, got.CreatedAt.Equal(firstCreated))

	// Total row count must be exactly 1.
	var count int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT count(*) FROM anomaly_alert
WHERE kind=$1 AND target_kind=$2 AND target_id=$3`,
		"on_time_rate_drop", "vendor", vendorID,
	).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestAnomalyRepo_Open_AfterClose_CreatesNew(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	a1 := &compliance.Anomaly{
		Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityMedium,
	}
	require.NoError(t, repo.Open(ctx, a1))
	firstID := a1.ID
	require.NoError(t, repo.Close(ctx, firstID, admin, "fixed"))

	// Now opening again for same key should INSERT a brand new row, because
	// the partial unique index only covers status='open'.
	a2 := &compliance.Anomaly{
		Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityHigh,
	}
	require.NoError(t, repo.Open(ctx, a2))
	assert.NotEqual(t, firstID, a2.ID, "must create a new row once the previous one was closed")

	var count int
	require.NoError(t, pool.QueryRow(ctx, `
SELECT count(*) FROM anomaly_alert
WHERE kind=$1 AND target_kind=$2 AND target_id=$3`,
		"on_time_rate_drop", "vendor", vendorID,
	).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestAnomalyRepo_Triage(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	a := &compliance.Anomaly{
		Kind: "doc_expired", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityLow,
	}
	require.NoError(t, repo.Open(ctx, a))
	require.NoError(t, repo.Triage(ctx, a.ID, admin, "investigating"))

	got, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.AnomalyTriaged, got.Status)
	require.NotNil(t, got.TriagedBy)
	assert.Equal(t, admin, *got.TriagedBy)
	require.NotNil(t, got.TriagedAt)
	assert.Equal(t, "investigating", got.Notes)

	// Re-triage on already-triaged anomaly returns invalid status.
	err = repo.Triage(ctx, a.ID, admin, "again")
	assert.ErrorIs(t, err, compliance.ErrInvalidStatus)
}

func TestAnomalyRepo_Close(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	a := &compliance.Anomaly{
		Kind: "doc_expired", TargetKind: "vendor", TargetID: vendorID,
		Severity: compliance.SeverityLow,
	}
	require.NoError(t, repo.Open(ctx, a))
	require.NoError(t, repo.Close(ctx, a.ID, admin, "resolved"))

	got, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.AnomalyClosed, got.Status)
	require.NotNil(t, got.ClosedBy)
	assert.Equal(t, admin, *got.ClosedBy)
	require.NotNil(t, got.ClosedAt)
	assert.Equal(t, "resolved", got.Notes)

	// Cannot re-close a closed anomaly.
	err = repo.Close(ctx, a.ID, admin, "again")
	assert.ErrorIs(t, err, compliance.ErrInvalidStatus)
}

func TestAnomalyRepo_List_FilteredByStatus(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	v1 := seedApprovedVendor(t, pool)
	v2 := seedApprovedVendor(t, pool)
	v3 := seedApprovedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	openA := &compliance.Anomaly{Kind: "k", TargetKind: "vendor", TargetID: v1, Severity: compliance.SeverityLow}
	triagedA := &compliance.Anomaly{Kind: "k", TargetKind: "vendor", TargetID: v2, Severity: compliance.SeverityHigh}
	closedA := &compliance.Anomaly{Kind: "k", TargetKind: "vendor", TargetID: v3, Severity: compliance.SeverityMedium}

	require.NoError(t, repo.Open(ctx, openA))
	require.NoError(t, repo.Open(ctx, triagedA))
	require.NoError(t, repo.Triage(ctx, triagedA.ID, admin, ""))
	require.NoError(t, repo.Open(ctx, closedA))
	require.NoError(t, repo.Close(ctx, closedA.ID, admin, ""))

	// All
	all, err := repo.List(ctx, nil, nil)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// Open only
	openOnly, err := repo.List(ctx, []compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	require.Len(t, openOnly, 1)
	assert.Equal(t, openA.ID, openOnly[0].ID)

	// Open + Triaged
	openOrTriaged, err := repo.List(ctx,
		[]compliance.AnomalyStatus{compliance.AnomalyOpen, compliance.AnomalyTriaged}, nil)
	require.NoError(t, err)
	require.Len(t, openOrTriaged, 2)

	// Filter by severity only
	high, err := repo.List(ctx, nil, []compliance.AnomalySeverity{compliance.SeverityHigh})
	require.NoError(t, err)
	require.Len(t, high, 1)
	assert.Equal(t, triagedA.ID, high[0].ID)

	// Combined status + severity
	openLow, err := repo.List(ctx,
		[]compliance.AnomalyStatus{compliance.AnomalyOpen},
		[]compliance.AnomalySeverity{compliance.SeverityLow})
	require.NoError(t, err)
	require.Len(t, openLow, 1)
	assert.Equal(t, openA.ID, openLow[0].ID)
}
