package feedback_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	cpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/postgres"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/postgres"
)

func newScanner(pool *pgxpool.Pool) (*feedback.FeedbackScanner, *cpg.AnomalyRepo) {
	anomRepo := cpg.NewAnomalyRepo(pool)
	return &feedback.FeedbackScanner{
		Ratings:    fpg.NewRatingRepo(pool),
		Complaints: fpg.NewComplaintRepo(pool),
		Anomaly:    anomRepo,
		Clock:      fixedClock{T: defaultNow},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Window:     14 * 24 * time.Hour,
	}, anomRepo
}

// insertRatingRow seeds a rating directly with a chosen score.
func insertRatingRow(t *testing.T, pool *pgxpool.Pool, user, vendor string, score int) {
	t.Helper()
	ctx := context.Background()
	orderID := seedPickedUpOrder(t, pool, user, vendor)
	repo := fpg.NewRatingRepo(pool)
	r := &feedback.Rating{OrderID: orderID, UserID: user, VendorID: vendor, Score: score}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, r)
	}))
}

// insertComplaintRow seeds an open complaint directly.
func insertComplaintRow(t *testing.T, pool *pgxpool.Pool, user, vendor string) {
	t.Helper()
	ctx := context.Background()
	orderID := seedPickedUpOrder(t, pool, user, vendor)
	repo := fpg.NewComplaintRepo(pool)
	c := &feedback.Complaint{
		OrderID: orderID, UserID: user, VendorID: vendor,
		Category: feedback.CategoryQuality, Description: "scanner seed complaint",
	}
	require.NoError(t, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		return repo.CreateTx(ctx, tx, c)
	}))
}

func findAnomaly(t *testing.T, anomRepo *cpg.AnomalyRepo, kind, vendorID string) *compliance.Anomaly {
	t.Helper()
	anomalies, err := anomRepo.List(context.Background(),
		[]compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	for _, a := range anomalies {
		if a.Kind == kind && a.TargetID == vendorID {
			return a
		}
	}
	return nil
}

func TestScanner_SatisfactionDrop_OpensMediumAnomaly(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	// 5 ratings averaging 3.0 (< 3.5, >= 2.5) → satisfaction_drop, medium.
	for _, s := range []int{2, 3, 3, 3, 4} {
		insertRatingRow(t, pool, user, vendor, s)
	}

	scanner, anomRepo := newScanner(pool)
	opened, err := scanner.RunOnce(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, opened, 1)

	a := findAnomaly(t, anomRepo, "satisfaction_drop", vendor)
	require.NotNil(t, a, "expected a satisfaction_drop anomaly")
	assert.Equal(t, compliance.SeverityMedium, a.Severity)
	assert.Equal(t, "vendor", a.TargetKind)
	assert.Equal(t, float64(5), a.Payload["sample_count"])
}

func TestScanner_SatisfactionDrop_HighSeverity(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	// 5 ratings averaging 2.0 (< 2.5) → high severity.
	for _, s := range []int{1, 2, 2, 2, 3} {
		insertRatingRow(t, pool, user, vendor, s)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "satisfaction_drop", vendor)
	require.NotNil(t, a)
	assert.Equal(t, compliance.SeverityHigh, a.Severity)
}

func TestScanner_SatisfactionDrop_NotEnoughSamples(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	// Only 4 low ratings — below the 5-sample minimum, no anomaly.
	for _, s := range []int{1, 1, 1, 1} {
		insertRatingRow(t, pool, user, vendor, s)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "satisfaction_drop", vendor)
	assert.Nil(t, a, "fewer than 5 samples must not trigger an anomaly")
}

func TestScanner_SatisfactionDrop_HealthyVendorNotFlagged(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	// 5 ratings averaging 4.4 (>= 3.5) → healthy, no anomaly.
	for _, s := range []int{4, 4, 5, 4, 5} {
		insertRatingRow(t, pool, user, vendor, s)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "satisfaction_drop", vendor)
	assert.Nil(t, a)
}

func TestScanner_ComplaintSpike_OpensMediumAnomaly(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	// 5 complaints (>= 5, < 10) → complaint_spike, medium.
	for i := 0; i < 5; i++ {
		insertComplaintRow(t, pool, user, vendor)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "complaint_spike", vendor)
	require.NotNil(t, a)
	assert.Equal(t, compliance.SeverityMedium, a.Severity)
	assert.Equal(t, float64(5), a.Payload["complaint_count"])
}

func TestScanner_ComplaintSpike_HighSeverity(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	for i := 0; i < 10; i++ {
		insertComplaintRow(t, pool, user, vendor)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "complaint_spike", vendor)
	require.NotNil(t, a)
	assert.Equal(t, compliance.SeverityHigh, a.Severity)
}

func TestScanner_ComplaintSpike_BelowThresholdNotFlagged(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	for i := 0; i < 4; i++ {
		insertComplaintRow(t, pool, user, vendor)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "complaint_spike", vendor)
	assert.Nil(t, a, "fewer than 5 complaints must not trigger an anomaly")
}

// TestScanner_Dedup verifies a second scan does not create a duplicate open
// anomaly — the partial unique index turns the second Open into an upsert.
func TestScanner_Dedup(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	for i := 0; i < 6; i++ {
		insertComplaintRow(t, pool, user, vendor)
	}

	scanner, anomRepo := newScanner(pool)
	_, err := scanner.RunOnce(ctx)
	require.NoError(t, err)
	_, err = scanner.RunOnce(ctx)
	require.NoError(t, err)

	anomalies, err := anomRepo.List(ctx, []compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	count := 0
	for _, a := range anomalies {
		if a.Kind == "complaint_spike" && a.TargetID == vendor {
			count++
		}
	}
	assert.Equal(t, 1, count, "repeated scans must not duplicate the open anomaly")
}

// TestScanner_WindowExcludesOldData verifies ratings outside the rolling
// window are not aggregated.
func TestScanner_WindowExcludesOldData(t *testing.T) {
	pool, _, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	for _, s := range []int{1, 1, 1, 1, 1} {
		insertRatingRow(t, pool, user, vendor, s)
	}
	// Push all ratings 30 days into the past — outside the 14-day window.
	_, err := pool.Exec(ctx, `UPDATE meal_rating SET created_at = $1 WHERE vendor_id = $2`,
		defaultNow.Add(-30*24*time.Hour), vendor)
	require.NoError(t, err)

	scanner, anomRepo := newScanner(pool)
	_, err = scanner.RunOnce(ctx)
	require.NoError(t, err)

	a := findAnomaly(t, anomRepo, "satisfaction_drop", vendor)
	assert.Nil(t, a, "ratings outside the window must not trigger an anomaly")
}
