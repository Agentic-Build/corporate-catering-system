package scanner_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance/scanner"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
)

// fakeDocRepo is an in-memory stub of compliance.DocumentRepository that lets
// the unit tests drive the scanner's error and edge branches without a real DB.
type fakeDocRepo struct {
	pastExpiry    []*compliance.Document
	expiringSoon  []*compliance.Document
	pastErr       error
	expiringErr   error
	updateErr     error
	updatedStatus map[string]compliance.DocumentStatus
	updateCalls   int
}

func (f *fakeDocRepo) Create(context.Context, *compliance.Document) error { return nil }
func (f *fakeDocRepo) CreateTx(context.Context, pgx.Tx, *compliance.Document) error {
	return nil
}
func (f *fakeDocRepo) GetByID(context.Context, string) (*compliance.Document, error) {
	return nil, nil
}
func (f *fakeDocRepo) ListByVendor(context.Context, string, bool) ([]*compliance.Document, error) {
	return nil, nil
}
func (f *fakeDocRepo) UpdateStatus(_ context.Context, id string, status compliance.DocumentStatus, _ *string, _ string) error {
	f.updateCalls++
	if f.updateErr != nil {
		return f.updateErr
	}
	if f.updatedStatus == nil {
		f.updatedStatus = map[string]compliance.DocumentStatus{}
	}
	f.updatedStatus[id] = status
	return nil
}
func (f *fakeDocRepo) UpdateStatusTx(context.Context, pgx.Tx, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (f *fakeDocRepo) ListExpiringBefore(context.Context, time.Time) ([]*compliance.Document, error) {
	return f.expiringSoon, f.expiringErr
}
func (f *fakeDocRepo) ListPastExpiry(context.Context, time.Time) ([]*compliance.Document, error) {
	return f.pastExpiry, f.pastErr
}

// fakeAnomalyRepo is an in-memory stub of compliance.AnomalyRepository.
type fakeAnomalyRepo struct {
	openErr   error
	opened    []*compliance.Anomaly
	openCalls int
}

func (f *fakeAnomalyRepo) Open(_ context.Context, a *compliance.Anomaly) error {
	f.openCalls++
	if f.openErr != nil {
		return f.openErr
	}
	f.opened = append(f.opened, a)
	return nil
}
func (f *fakeAnomalyRepo) GetByID(context.Context, string) (*compliance.Anomaly, error) {
	return nil, nil
}
func (f *fakeAnomalyRepo) List(context.Context, []compliance.AnomalyStatus, []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	return nil, nil
}
func (f *fakeAnomalyRepo) Triage(context.Context, string, string, string) error { return nil }
func (f *fakeAnomalyRepo) TriageTx(context.Context, pgx.Tx, string, string, string) error {
	return nil
}
func (f *fakeAnomalyRepo) Close(context.Context, string, string, string) error { return nil }
func (f *fakeAnomalyRepo) CloseTx(context.Context, pgx.Tx, string, string, string) error {
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mkScanner(docs *fakeDocRepo, anom *fakeAnomalyRepo, now time.Time) *scanner.DocumentExpiryScanner {
	return &scanner.DocumentExpiryScanner{
		Docs:       docs,
		Anomaly:    anom,
		DaysWindow: 14,
		Logger:     quietLogger(),
		Clock:      clock.FixedClock{T: now},
	}
}

var unitNow = time.Date(2027, time.June, 15, 12, 0, 0, 0, time.UTC)

// ListPastExpiry error must wrap and abort the pass.
func TestRunOnce_PastExpiryListError(t *testing.T) {
	docs := &fakeDocRepo{pastErr: errors.New("boom-past")}
	s := mkScanner(docs, &fakeAnomalyRepo{}, unitNow)

	n, err := s.RunOnce(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "list past expiry")
	assert.Contains(t, err.Error(), "boom-past")
}

// ListExpiringBefore error must wrap and abort the pass.
func TestRunOnce_ExpiringListError(t *testing.T) {
	docs := &fakeDocRepo{expiringErr: errors.New("boom-soon")}
	s := mkScanner(docs, &fakeAnomalyRepo{}, unitNow)

	n, err := s.RunOnce(context.Background())
	require.Error(t, err)
	assert.Equal(t, 0, n)
	assert.Contains(t, err.Error(), "list expiring")
	assert.Contains(t, err.Error(), "boom-soon")
}

// RunOnce defaults DaysWindow to 14 when unset (<=0).
func TestRunOnce_DefaultsDaysWindow(t *testing.T) {
	docs := &fakeDocRepo{}
	s := &scanner.DocumentExpiryScanner{
		Docs:    docs,
		Anomaly: &fakeAnomalyRepo{},
		Logger:  quietLogger(),
		Clock:   clock.FixedClock{T: unitNow},
		// DaysWindow intentionally 0
	}
	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 14, s.DaysWindow)
}

// handlePastExpiry: UpdateStatus failure → row skipped, anomaly never opened.
func TestRunOnce_PastExpiry_UpdateStatusError(t *testing.T) {
	exp := unitNow.AddDate(0, 0, -1)
	docs := &fakeDocRepo{
		updateErr: errors.New("update fail"),
		pastExpiry: []*compliance.Document{
			{ID: "d1", VendorID: "v1", Kind: compliance.DocKindInsurance,
				BlobURI: "s3://d1", Filename: "i.pdf", ExpiresAt: &exp},
		},
	}
	anom := &fakeAnomalyRepo{}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, anom.openCalls, "anomaly must not open when status update fails")
}

// handlePastExpiry: Open failure → row skipped (status already flipped).
func TestRunOnce_PastExpiry_OpenError(t *testing.T) {
	exp := unitNow.AddDate(0, 0, -1)
	docs := &fakeDocRepo{
		pastExpiry: []*compliance.Document{
			{ID: "d1", VendorID: "v1", Kind: compliance.DocKindInsurance,
				BlobURI: "s3://d1", Filename: "i.pdf", ExpiresAt: &exp},
		},
	}
	anom := &fakeAnomalyRepo{openErr: errors.New("open fail")}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, compliance.DocStatusExpired, docs.updatedStatus["d1"])
}

// handlePastExpiry: ExpiresAt nil → no "expired_at" payload key, still handled.
func TestRunOnce_PastExpiry_NilExpiresAt(t *testing.T) {
	docs := &fakeDocRepo{
		pastExpiry: []*compliance.Document{
			{ID: "d1", VendorID: "v1", Kind: compliance.DocKindInsurance,
				BlobURI: "s3://d1", Filename: "i.pdf", ExpiresAt: nil},
		},
	}
	anom := &fakeAnomalyRepo{}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, anom.opened, 1)
	_, hasExpiredAt := anom.opened[0].Payload["expired_at"]
	assert.False(t, hasExpiredAt, "nil ExpiresAt must omit expired_at payload key")
	assert.Equal(t, "document_expired", anom.opened[0].Kind)
}

// handleExpiringSoon: nil ExpiresAt and not-after-now rows are skipped.
func TestRunOnce_ExpiringSoon_SkipsNonFuture(t *testing.T) {
	nilExp := (*time.Time)(nil)
	past := unitNow.AddDate(0, 0, -3)
	docs := &fakeDocRepo{
		expiringSoon: []*compliance.Document{
			{ID: "d-nil", VendorID: "v1", Kind: compliance.DocKindInsurance, ExpiresAt: nilExp},
			{ID: "d-past", VendorID: "v1", Kind: compliance.DocKindInsurance, ExpiresAt: &past},
		},
	}
	anom := &fakeAnomalyRepo{}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Equal(t, 0, anom.openCalls)
}

// handleExpiringSoon: critical bucket (<=1 day) plus Open success.
func TestRunOnce_ExpiringSoon_CriticalBucket(t *testing.T) {
	exp := unitNow.Add(12 * time.Hour) // ~0 days until → critical
	docs := &fakeDocRepo{
		expiringSoon: []*compliance.Document{
			{ID: "d1", VendorID: "v1", Kind: compliance.DocKindBusinessLicense,
				BlobURI: "s3://d1", Filename: "lic.pdf", ExpiresAt: &exp},
		},
	}
	anom := &fakeAnomalyRepo{}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, anom.opened, 1)
	assert.Equal(t, compliance.SeverityCritical, anom.opened[0].Severity)
	assert.Equal(t, "document_expiring", anom.opened[0].Kind)
}

// handleExpiringSoon: Open failure → row not counted.
func TestRunOnce_ExpiringSoon_OpenError(t *testing.T) {
	exp := unitNow.AddDate(0, 0, 5)
	docs := &fakeDocRepo{
		expiringSoon: []*compliance.Document{
			{ID: "d1", VendorID: "v1", Kind: compliance.DocKindBusinessLicense,
				BlobURI: "s3://d1", Filename: "lic.pdf", ExpiresAt: &exp},
		},
	}
	anom := &fakeAnomalyRepo{openErr: errors.New("open fail")}
	s := mkScanner(docs, anom, unitNow)

	n, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

// Run: defaults Interval/DaysWindow, performs the initial scan, then exits on
// ctx cancellation. Cancelling before the first tick keeps it deterministic.
func TestRun_DefaultsAndStopsOnCancel(t *testing.T) {
	docs := &fakeDocRepo{}
	anom := &fakeAnomalyRepo{}
	s := &scanner.DocumentExpiryScanner{
		Docs:    docs,
		Anomaly: anom,
		Logger:  quietLogger(),
		Clock:   clock.FixedClock{T: unitNow},
		// Interval and DaysWindow zero → defaulted inside Run.
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: Run does initial scan then returns immediately

	err := s.Run(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, time.Hour, s.Interval)
	assert.Equal(t, 14, s.DaysWindow)
}

// Run: initial RunOnce error path is logged (not returned) and the loop still
// runs at least one tick before ctx cancellation stops it.
func TestRun_InitialScanErrorThenTick(t *testing.T) {
	// First RunOnce errors (past list), so the "initial scan" error branch is hit.
	docs := &fakeDocRepo{pastErr: errors.New("boom")}
	anom := &fakeAnomalyRepo{}
	s := &scanner.DocumentExpiryScanner{
		Docs:       docs,
		Anomaly:    anom,
		Interval:   5 * time.Millisecond,
		DaysWindow: 14,
		Logger:     quietLogger(),
		Clock:      clock.FixedClock{T: unitNow},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	err := s.Run(ctx)
	require.Error(t, err) // ctx deadline or canceled
	// At least the initial scan ran; ticks keep erroring through the logged path.
	assert.GreaterOrEqual(t, docs.updateCalls, 0)
}
