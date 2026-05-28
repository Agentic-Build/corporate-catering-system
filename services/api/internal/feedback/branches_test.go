package feedback_test

import (
	"context"
	"errors"
	audit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
)

// These tests exercise the validation / not-found / repo-error branches with
// in-memory fakes (no testcontainers) so the error paths run fast and without a
// real DB. The fakeBeginner hands the write closure a no-op pgx.Tx; the repo
// fakes ignore it.

var errBoom = errors.New("boom")

// === fakes ===

type fbBeginner struct{}

func (fbBeginner) Begin(context.Context) (pgx.Tx, error) { return fbTx{}, nil }

type fbTx struct{ pgx.Tx }

func (fbTx) Commit(context.Context) error   { return nil }
func (fbTx) Rollback(context.Context) error { return nil }

type fakeRatingRepo struct {
	getByOrder    *feedback.Rating
	getByOrderErr error
	createErr     error
	aggStats      []feedback.VendorRatingStat
	aggErr        error
}

func (r *fakeRatingRepo) CreateTx(context.Context, pgx.Tx, *feedback.Rating) error {
	return r.createErr
}
func (r *fakeRatingRepo) GetByOrder(context.Context, string) (*feedback.Rating, error) {
	return r.getByOrder, r.getByOrderErr
}
func (r *fakeRatingRepo) AggregateByVendorSince(context.Context, time.Time) ([]feedback.VendorRatingStat, error) {
	return r.aggStats, r.aggErr
}

type fakeComplaintRepo struct {
	getByID    *feedback.Complaint
	getByIDErr error
	createErr  error
	updateErr  error
	countStats []feedback.VendorComplaintStat
	countErr   error
}

func (r *fakeComplaintRepo) CreateTx(context.Context, pgx.Tx, *feedback.Complaint) error {
	return r.createErr
}
func (r *fakeComplaintRepo) GetByID(context.Context, string) (*feedback.Complaint, error) {
	return r.getByID, r.getByIDErr
}
func (r *fakeComplaintRepo) UpdateStatusTx(context.Context, pgx.Tx, string, feedback.ComplaintStatus, feedback.ComplaintStatus, feedback.ComplaintUpdate) error {
	return r.updateErr
}
func (r *fakeComplaintRepo) ListByUser(context.Context, string) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (r *fakeComplaintRepo) ListByVendor(context.Context, string, []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (r *fakeComplaintRepo) ListByStatus(context.Context, []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (r *fakeComplaintRepo) CountByVendorSince(context.Context, time.Time) ([]feedback.VendorComplaintStat, error) {
	return r.countStats, r.countErr
}

type fakeOrderReader struct {
	info *feedback.OrderInfo
	err  error
}

func (r *fakeOrderReader) GetOrderInfo(context.Context, string) (*feedback.OrderInfo, error) {
	return r.info, r.err
}

type fakeAudit struct{ err error }

func (a fakeAudit) WriteTx(context.Context, pgx.Tx, audit.Entry) error {
	return a.err
}

// fakeAnomalyRepo embeds the interface so only Open needs an implementation.
type fakeAnomalyRepo struct {
	compliance.AnomalyRepository
	openErr error
}

func (r *fakeAnomalyRepo) Open(context.Context, *compliance.Anomaly) error { return r.openErr }

func pickedUp(userID, vendorID string) *feedback.OrderInfo {
	return &feedback.OrderInfo{ID: "o-1", UserID: userID, VendorID: vendorID, Status: "picked_up"}
}

func newSvc(orders *fakeOrderReader, ratings *fakeRatingRepo, complaints *fakeComplaintRepo, audit fakeAudit) *feedback.Service {
	return &feedback.Service{
		Pool:       fbBeginner{},
		Ratings:    ratings,
		Complaints: complaints,
		Orders:     orders,
		Audit:      audit,
		Clock:      fixedClock{T: defaultNow},
	}
}

// === RateOrder branches ===

func TestRateOrder_CommentTooLong(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	long := make([]byte, 501)
	for i := range long {
		long[i] = 'a'
	}
	_, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{
		OrderID: "o-1", UserID: "u-1", Score: 3, Comment: string(long),
	})
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

func TestRateOrder_OrderLookupError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{err: errBoom}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{OrderID: "o-1", UserID: "u-1", Score: 3})
	assert.ErrorIs(t, err, errBoom)
}

func TestRateOrder_GetByOrderError(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{getByOrderErr: errBoom},
		&fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{OrderID: "o-1", UserID: "u-1", Score: 3})
	assert.ErrorIs(t, err, errBoom)
}

func TestRateOrder_CreateTxError(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{getByOrderErr: feedback.ErrRatingNotFound, createErr: errBoom},
		&fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{OrderID: "o-1", UserID: "u-1", Score: 3})
	assert.ErrorIs(t, err, errBoom)
}

func TestRateOrder_CreateTxUniqueViolation(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{getByOrderErr: feedback.ErrRatingNotFound, createErr: errors.New("meal_rating_order_id_key dup")},
		&fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{OrderID: "o-1", UserID: "u-1", Score: 3})
	assert.ErrorIs(t, err, feedback.ErrAlreadyRated)
}

func TestRateOrder_Happy_Fake(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{getByOrderErr: feedback.ErrRatingNotFound},
		&fakeComplaintRepo{}, fakeAudit{})
	r, err := svc.RateOrder(context.Background(), feedback.RateOrderInput{OrderID: "o-1", UserID: "u-1", Score: 5})
	require.NoError(t, err)
	assert.Equal(t, "v-1", r.VendorID)
}

// === GetRating ===

func TestGetRating_Found(t *testing.T) {
	want := &feedback.Rating{ID: "r-1", OrderID: "o-1"}
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{getByOrder: want}, &fakeComplaintRepo{}, fakeAudit{})
	got, err := svc.GetRating(context.Background(), "o-1")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetRating_NotFound(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{getByOrderErr: feedback.ErrRatingNotFound}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.GetRating(context.Background(), "o-1")
	assert.ErrorIs(t, err, feedback.ErrRatingNotFound)
}

// === FileComplaint branches ===

func TestFileComplaint_InvalidCategory(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "u-1", Category: "bogus", Description: "a valid description",
	})
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

func TestFileComplaint_OrderLookupError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{err: errBoom}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "u-1", Category: feedback.CategoryQuality, Description: "a valid description",
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestFileComplaint_NotOwner(t *testing.T) {
	svc := newSvc(&fakeOrderReader{info: pickedUp("owner", "v-1")}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "intruder", Category: feedback.CategoryQuality, Description: "a valid description",
	})
	assert.ErrorIs(t, err, feedback.ErrForbidden)
}

func TestFileComplaint_CreateUniqueViolation(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{},
		&fakeComplaintRepo{createErr: errors.New("meal_complaint_one_open_idx dup")}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "u-1", Category: feedback.CategoryQuality, Description: "a valid description",
	})
	assert.ErrorIs(t, err, feedback.ErrComplaintExists)
}

// === workflow GetByID error branches ===

func TestRespondToComplaint_GetByIDError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByIDErr: errBoom}, fakeAudit{})
	err := svc.RespondToComplaint(context.Background(), "c-1", "v-1", "op-1", "a valid response")
	assert.ErrorIs(t, err, errBoom)
}

func TestEscalateComplaint_GetByIDError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByIDErr: errBoom}, fakeAudit{})
	err := svc.EscalateComplaint(context.Background(), "c-1", "u-1")
	assert.ErrorIs(t, err, errBoom)
}

func TestEmployeeResolveComplaint_GetByIDError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByIDErr: errBoom}, fakeAudit{})
	err := svc.EmployeeResolveComplaint(context.Background(), "c-1", "u-1")
	assert.ErrorIs(t, err, errBoom)
}

func TestEmployeeResolveComplaint_NotOwner(t *testing.T) {
	c := &feedback.Complaint{ID: "c-1", UserID: "owner", Status: feedback.StatusOpen}
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByID: c}, fakeAudit{})
	err := svc.EmployeeResolveComplaint(context.Background(), "c-1", "intruder")
	assert.ErrorIs(t, err, feedback.ErrForbidden)
}

func TestAdminResolveComplaint_GetByIDError(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByIDErr: errBoom}, fakeAudit{})
	err := svc.AdminResolveComplaint(context.Background(), "c-1", "a-1", "a valid resolution", false)
	assert.ErrorIs(t, err, errBoom)
}

// transition error: UpdateStatusTx fails after the escalated/resolution gate.
func TestAdminResolveComplaint_TransitionError(t *testing.T) {
	c := &feedback.Complaint{ID: "c-1", OrderID: "o-1", VendorID: "v-1", Status: feedback.StatusEscalated}
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{getByID: c, updateErr: errBoom}, fakeAudit{})
	err := svc.AdminResolveComplaint(context.Background(), "c-1", "a-1", "a valid resolution", false)
	assert.ErrorIs(t, err, errBoom)
}

// === validCategory default + mapUniqueViolation passthrough ===

func TestFileComplaint_EmptyCategory_Invalid(t *testing.T) {
	svc := newSvc(&fakeOrderReader{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "u-1", Category: "", Description: "a valid description",
	})
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

// CreateTx returns a non-unique error → mapUniqueViolation passes it through.
func TestFileComplaint_CreateGenericError(t *testing.T) {
	svc := newSvc(
		&fakeOrderReader{info: pickedUp("u-1", "v-1")},
		&fakeRatingRepo{},
		&fakeComplaintRepo{createErr: errBoom}, fakeAudit{})
	_, err := svc.FileComplaint(context.Background(), feedback.FileComplaintInput{
		OrderID: "o-1", UserID: "u-1", Category: feedback.CategoryOther, Description: "a valid description",
	})
	assert.ErrorIs(t, err, errBoom)
}

// === scanner: Run loop + log helpers + RunOnce error paths ===

func newFakeScanner(ratings *fakeRatingRepo, complaints *fakeComplaintRepo, anom *fakeAnomalyRepo, logger *slog.Logger) *feedback.FeedbackScanner {
	return &feedback.FeedbackScanner{
		Ratings:    ratings,
		Complaints: complaints,
		Anomaly:    anom,
		Clock:      fixedClock{T: defaultNow},
		Logger:     logger,
		Window:     14 * 24 * time.Hour,
	}
}

func TestScannerRunOnce_AggregateRatingsError(t *testing.T) {
	s := newFakeScanner(&fakeRatingRepo{aggErr: errBoom}, &fakeComplaintRepo{}, &fakeAnomalyRepo{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := s.RunOnce(context.Background())
	assert.ErrorIs(t, err, errBoom)
}

func TestScannerRunOnce_CountComplaintsError(t *testing.T) {
	s := newFakeScanner(&fakeRatingRepo{}, &fakeComplaintRepo{countErr: errBoom}, &fakeAnomalyRepo{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := s.RunOnce(context.Background())
	assert.ErrorIs(t, err, errBoom)
}

// Anomaly.Open failing on both signals exercises logWarn and the continue path:
// opened stays 0 even though both vendors breach thresholds.
func TestScannerRunOnce_OpenError_LogsWarnAndSkips(t *testing.T) {
	ratings := &fakeRatingRepo{aggStats: []feedback.VendorRatingStat{
		{VendorID: "v-1", AvgScore: 1.5, SampleCount: 10},
	}}
	complaints := &fakeComplaintRepo{countStats: []feedback.VendorComplaintStat{
		{VendorID: "v-2", Count: 12},
	}}
	s := newFakeScanner(ratings, complaints, &fakeAnomalyRepo{openErr: errBoom},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	opened, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, opened)
}

// DefaultWindow path: Window<=0 falls back to the 14d default.
func TestScannerRunOnce_DefaultWindow(t *testing.T) {
	s := &feedback.FeedbackScanner{
		Ratings:    &fakeRatingRepo{},
		Complaints: &fakeComplaintRepo{},
		Anomaly:    &fakeAnomalyRepo{},
		Clock:      fixedClock{T: defaultNow},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Window left zero on purpose.
	}
	opened, err := s.RunOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, opened)
}

// Run: an already-cancelled context returns promptly after the initial scan,
// exercising the Interval/Window defaulting and the ctx.Done() branch.
func TestScannerRun_CancelledContext(t *testing.T) {
	s := &feedback.FeedbackScanner{
		Ratings:    &fakeRatingRepo{aggErr: errBoom}, // initial scan logs an error
		Complaints: &fakeComplaintRepo{},
		Anomaly:    &fakeAnomalyRepo{},
		Clock:      fixedClock{T: defaultNow},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:   time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// Run with Interval/Window left zero exercises the defaulting branches; the
// already-cancelled context returns before the (1h) ticker ever fires.
func TestScannerRun_DefaultIntervalCancelled(t *testing.T) {
	s := &feedback.FeedbackScanner{
		Ratings:    &fakeRatingRepo{},
		Complaints: &fakeComplaintRepo{},
		Anomaly:    &fakeAnomalyRepo{},
		Clock:      fixedClock{T: defaultNow},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		// Interval and Window left zero on purpose.
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Run(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// Run with a live ticker: cancel after the first tick to cover the ticker.C
// branch (tick scan error logged) as well as ctx.Done().
func TestScannerRun_TickThenCancel(t *testing.T) {
	s := &feedback.FeedbackScanner{
		Ratings:    &fakeRatingRepo{aggErr: errBoom},
		Complaints: &fakeComplaintRepo{},
		Anomaly:    &fakeAnomalyRepo{},
		Clock:      fixedClock{T: defaultNow},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:   5 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Run(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
