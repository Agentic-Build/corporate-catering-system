package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/postgres"
)

// Open with empty Severity and nil Payload exercises the default-severity and
// default-payload branches, and verifies the receiver is normalized.
func TestAnomalyRepo_Open_Defaults(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := pgrepo.NewAnomalyRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	a := &compliance.Anomaly{
		Kind:       "on_time_rate_drop",
		TargetKind: "vendor",
		TargetID:   vendorID,
		// Severity empty, Payload nil, EvidenceURI nil.
	}
	require.NoError(t, repo.Open(ctx, a))
	require.NotEmpty(t, a.ID)
	// Receiver normalized in place.
	assert.Equal(t, compliance.SeverityMedium, a.Severity)
	assert.Equal(t, compliance.AnomalyOpen, a.Status)
	assert.NotNil(t, a.Payload)
	assert.NotNil(t, a.EvidenceURI)

	got, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.SeverityMedium, got.Severity)
	assert.Empty(t, got.EvidenceURI)
}

// A payload containing a non-marshalable value forces json.Marshal to fail,
// covering the marshal error path in Open.
func TestAnomalyRepo_Open_MarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	repo := pgrepo.NewAnomalyRepo(pool)

	a := &compliance.Anomaly{
		Kind:       "k",
		TargetKind: "vendor",
		TargetID:   "00000000-0000-0000-0000-000000000000",
		Payload:    map[string]any{"bad": make(chan int)},
	}
	err := repo.Open(context.Background(), a)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal anomaly payload")
}

// Triggering DB errors via a closed pool covers the non-NoRows error branches
// across both repos (QueryRow/Query/Exec failures).
func TestRepos_PoolClosed_Errors(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	anomalyRepo := pgrepo.NewAnomalyRepo(pool)
	docRepo := pgrepo.NewDocumentRepo(pool)
	id := "00000000-0000-0000-0000-000000000000"

	// Close the pool so every subsequent query returns a (non-NoRows) error.
	pool.Close()

	t.Run("AnomalyOpen", func(t *testing.T) {
		err := anomalyRepo.Open(ctx, &compliance.Anomaly{Kind: "k", TargetKind: "vendor", TargetID: id})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "open anomaly")
	})
	t.Run("AnomalyGetByID", func(t *testing.T) {
		_, err := anomalyRepo.GetByID(ctx, id)
		require.Error(t, err)
		assert.NotErrorIs(t, err, compliance.ErrAnomalyNotFound)
		assert.Contains(t, err.Error(), "scan anomaly")
	})
	t.Run("AnomalyList", func(t *testing.T) {
		_, err := anomalyRepo.List(ctx, nil, nil)
		require.Error(t, err)
	})
	t.Run("AnomalyTriage", func(t *testing.T) {
		err := anomalyRepo.Triage(ctx, id, "by", "n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "triage anomaly")
	})
	t.Run("AnomalyClose", func(t *testing.T) {
		err := anomalyRepo.Close(ctx, id, "by", "n")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "close anomaly")
	})
	t.Run("DocCreate", func(t *testing.T) {
		err := docRepo.Create(ctx, &compliance.Document{VendorID: id, Kind: compliance.DocKindOther, BlobURI: "s3://x", Filename: "x"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create document")
	})
	t.Run("DocGetByID", func(t *testing.T) {
		_, err := docRepo.GetByID(ctx, id)
		require.Error(t, err)
		assert.NotErrorIs(t, err, compliance.ErrDocumentNotFound)
		assert.Contains(t, err.Error(), "scan doc")
	})
	t.Run("DocListByVendor", func(t *testing.T) {
		_, err := docRepo.ListByVendor(ctx, id, true)
		require.Error(t, err)
	})
	t.Run("DocUpdateStatus", func(t *testing.T) {
		err := docRepo.UpdateStatus(ctx, id, compliance.DocStatusApproved, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update document status")
	})
}

// The Tx variants run the same statements inside a caller-owned transaction.
func TestRepos_TxVariants(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	anomalyRepo := pgrepo.NewAnomalyRepo(pool)
	docRepo := pgrepo.NewDocumentRepo(pool)

	vendorID := seedApprovedVendor(t, pool)
	admin := seedAdminUser(t, pool)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	// CreateTx -> doc gets an id within the tx.
	doc := &compliance.Document{
		VendorID: vendorID, Kind: compliance.DocKindBusinessLicense,
		BlobURI: "s3://tx", Filename: "tx.pdf",
	}
	require.NoError(t, docRepo.CreateTx(ctx, tx, doc))
	require.NotEmpty(t, doc.ID)
	assert.Equal(t, compliance.DocStatusPending, doc.Status)

	// UpdateStatusTx -> approve within the tx.
	require.NoError(t, docRepo.UpdateStatusTx(ctx, tx, doc.ID, compliance.DocStatusApproved, &admin, "ok"))

	// Open an anomaly directly (non-tx) so TriageTx/CloseTx have a target.
	// Use the same tx to keep everything atomic.
	var anomalyID string
	expires := time.Now().UTC()
	_ = expires
	require.NoError(t, tx.QueryRow(ctx, `
INSERT INTO anomaly_alert (kind, target_kind, target_id, severity, status, payload, evidence_uri)
VALUES ('k', 'vendor', $1, 'low'::anomaly_severity, 'open', '{}'::jsonb, '{}')
RETURNING id`, vendorID).Scan(&anomalyID))

	// TriageTx moves open -> triaged.
	require.NoError(t, anomalyRepo.TriageTx(ctx, tx, anomalyID, admin, "looking"))
	// CloseTx moves triaged -> closed.
	require.NoError(t, anomalyRepo.CloseTx(ctx, tx, anomalyID, admin, "done"))

	require.NoError(t, tx.Commit(ctx))

	// Verify committed state via the pool.
	gotDoc, err := docRepo.GetByID(ctx, doc.ID)
	require.NoError(t, err)
	assert.Equal(t, compliance.DocStatusApproved, gotDoc.Status)

	gotAnomaly, err := anomalyRepo.GetByID(ctx, anomalyID)
	require.NoError(t, err)
	assert.Equal(t, compliance.AnomalyClosed, gotAnomaly.Status)
}
