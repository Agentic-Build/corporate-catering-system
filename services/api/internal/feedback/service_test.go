package feedback_test

import (
	"context"
	"fmt"
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

	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	fpg "github.com/takalawang/corporate-catering-system/services/api/internal/feedback/postgres"
	opg "github.com/takalawang/corporate-catering-system/services/api/internal/order/postgres"
)

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

var defaultNow = time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

func migrationsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// services/api/internal/feedback/service_test.go → ../../../../migrations
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "migrations")
}

func setup(t *testing.T) (*pgxpool.Pool, *feedback.Service, func()) {
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
	m, err := migrate.New("file://"+migrationsDir(), dsn)
	require.NoError(t, err)
	require.NoError(t, m.Up())
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 20
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)

	svc := &feedback.Service{
		Pool:       pool,
		Ratings:    fpg.NewRatingRepo(pool),
		Complaints: fpg.NewComplaintRepo(pool),
		Orders:     fpg.NewOrderReader(pool),
		Audit:      opg.NewAuditRepo(pool),
		Clock:      fixedClock{T: defaultNow},
	}
	cleanup := func() {
		pool.Close()
		_ = container.Terminate(ctx)
	}
	return pool, svc, cleanup
}

var (
	userCounter   atomic.Uint64
	vendorCounter atomic.Uint64
)

func seedUserWithRole(t *testing.T, pool *pgxpool.Pool, role string) string {
	t.Helper()
	n := userCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "user" (primary_email, display_name, role)
VALUES ($1, $2, $3) RETURNING id`,
		fmt.Sprintf("feedback-svc-user-%d@test.com", n),
		fmt.Sprintf("feedback-svc-user-%d", n),
		role,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedEmployee(t *testing.T, pool *pgxpool.Pool) string {
	return seedUserWithRole(t, pool, "employee")
}

func seedAdmin(t *testing.T, pool *pgxpool.Pool) string {
	return seedUserWithRole(t, pool, "welfare_admin")
}

func seedVendor(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	n := vendorCounter.Add(1)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, 'approved') RETURNING id`,
		fmt.Sprintf("feedback-svc-vendor-%d", n),
		fmt.Sprintf("feedback-svc-vendor-%d Ltd", n),
		fmt.Sprintf("feedback-svc-vendor-%d@test.com", n),
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// seedOrderWithStatus inserts an order row directly so tests can land orders in
// any status.
func seedOrderWithStatus(t *testing.T, pool *pgxpool.Pool, userID, vendorID, status string) string {
	t.Helper()
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = 0xab
	}
	day := defaultNow.Truncate(24 * time.Hour)
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO "order"
  (user_id, vendor_id, plant, supply_date, status, total_price_minor, placed_at, cutoff_at, totp_secret)
VALUES ($1,$2,'F12B-3F',$3,$4::order_status,$5,$6,$7,$8) RETURNING id`,
		userID, vendorID, day, status, int64(12000),
		day.Add(-6*time.Hour), day.Add(-1*time.Hour), secret,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func seedPickedUpOrder(t *testing.T, pool *pgxpool.Pool, userID, vendorID string) string {
	return seedOrderWithStatus(t, pool, userID, vendorID, "picked_up")
}

// backdateComplaint pushes a complaint's created_at into the past so the 24h
// escalation gate is satisfied. This is a test-only mutation.
func backdateComplaint(t *testing.T, pool *pgxpool.Pool, complaintID string, age time.Duration) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE meal_complaint SET created_at = $2 WHERE id = $1`,
		complaintID, defaultNow.Add(-age))
	require.NoError(t, err)
}

// ---------- RateOrder ----------

func TestService_RateOrder_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	r, err := svc.RateOrder(ctx, feedback.RateOrderInput{
		OrderID: orderID, UserID: user, Score: 4, Comment: "good",
	})
	require.NoError(t, err)
	require.NotEmpty(t, r.ID)
	assert.Equal(t, 4, r.Score)
	assert.Equal(t, vendor, r.VendorID)

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='feedback.rate_order'`, r.ID).Scan(&auditCount))
	assert.Equal(t, 1, auditCount)
}

func TestService_RateOrder_NotPickedUp(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrderWithStatus(t, pool, user, vendor, "placed")

	_, err := svc.RateOrder(ctx, feedback.RateOrderInput{OrderID: orderID, UserID: user, Score: 5})
	assert.ErrorIs(t, err, feedback.ErrOrderNotPickedUp)
}

func TestService_RateOrder_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	owner := seedEmployee(t, pool)
	intruder := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, owner, vendor)

	_, err := svc.RateOrder(ctx, feedback.RateOrderInput{OrderID: orderID, UserID: intruder, Score: 5})
	assert.ErrorIs(t, err, feedback.ErrForbidden)
}

func TestService_RateOrder_DuplicateRejected(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	_, err := svc.RateOrder(ctx, feedback.RateOrderInput{OrderID: orderID, UserID: user, Score: 3})
	require.NoError(t, err)

	_, err = svc.RateOrder(ctx, feedback.RateOrderInput{OrderID: orderID, UserID: user, Score: 5})
	assert.ErrorIs(t, err, feedback.ErrAlreadyRated)
}

func TestService_RateOrder_InvalidScore(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	_, err := svc.RateOrder(ctx, feedback.RateOrderInput{OrderID: orderID, UserID: user, Score: 6})
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

// ---------- FileComplaint ----------

func TestService_FileComplaint_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	c, err := svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user,
		Category: feedback.CategoryMissingItem, Description: "missing the dessert item",
	})
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusOpen, c.Status)

	var auditCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE target_id=$1 AND action='feedback.file_complaint'`, c.ID).Scan(&auditCount))
	assert.Equal(t, 1, auditCount)
}

func TestService_FileComplaint_ShortDescription(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	_, err := svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user, Category: feedback.CategoryQuality, Description: "bad",
	})
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

func TestService_FileComplaint_NotPickedUp(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedOrderWithStatus(t, pool, user, vendor, "cancelled")

	_, err := svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user, Category: feedback.CategoryQuality, Description: "valid description here",
	})
	assert.ErrorIs(t, err, feedback.ErrOrderNotPickedUp)
}

func TestService_FileComplaint_DuplicateUnresolvedRejected(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)

	_, err := svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user, Category: feedback.CategoryQuality, Description: "first complaint here",
	})
	require.NoError(t, err)

	_, err = svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user, Category: feedback.CategoryPortion, Description: "second complaint here",
	})
	assert.ErrorIs(t, err, feedback.ErrComplaintExists)
}

// ---------- Workflow transitions ----------

func fileComplaint(t *testing.T, ctx context.Context, svc *feedback.Service, user, orderID string) *feedback.Complaint {
	t.Helper()
	c, err := svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID: orderID, UserID: user,
		Category: feedback.CategoryQuality, Description: "the meal had a problem",
	})
	require.NoError(t, err)
	return c
}

func TestService_RespondToComplaint_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	require.NoError(t, svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "we apologize and will improve"))

	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusVendorResponded, got.Status)
	assert.Equal(t, "we apologize and will improve", got.VendorResponse)
}

func TestService_RespondToComplaint_WrongVendor(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	otherVendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	err := svc.RespondToComplaint(ctx, c.ID, otherVendor, vendorOp, "we apologize for this")
	assert.ErrorIs(t, err, feedback.ErrForbidden)
}

func TestService_RespondToComplaint_ShortResponse(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	err := svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "ok")
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

func TestService_RespondToComplaint_NotOpen(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	require.NoError(t, svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "first response here"))
	// A second respond on an already-responded complaint is invalid.
	err := svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "second response here")
	assert.ErrorIs(t, err, feedback.ErrInvalidTransition)
}

func TestService_EscalateComplaint_TooEarly(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	// created_at is "now" (defaultNow); escalate gate is 24h.
	err := svc.EscalateComplaint(ctx, c.ID, user)
	assert.ErrorIs(t, err, feedback.ErrEscalateTooEarly)
}

func TestService_EscalateComplaint_AfterGate(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)

	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))
	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusEscalated, got.Status)
	require.NotNil(t, got.EscalatedAt)
}

func TestService_EscalateComplaint_FromVendorResponded(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	require.NoError(t, svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "vendor response text"))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)

	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))
	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusEscalated, got.Status)
}

func TestService_EscalateComplaint_NotOwner(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	intruder := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)

	err := svc.EscalateComplaint(ctx, c.ID, intruder)
	assert.ErrorIs(t, err, feedback.ErrForbidden)
}

func TestService_EscalateComplaint_AlreadyEscalated(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	err := svc.EscalateComplaint(ctx, c.ID, user)
	assert.ErrorIs(t, err, feedback.ErrInvalidTransition)
}

func TestService_EmployeeResolveComplaint_FromOpen(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	require.NoError(t, svc.EmployeeResolveComplaint(ctx, c.ID, user))
	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusResolved, got.Status)
	require.NotNil(t, got.ResolvedBy)
	assert.Equal(t, user, *got.ResolvedBy)
}

func TestService_EmployeeResolveComplaint_FromVendorResponded(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	vendorOp := seedUserWithRole(t, pool, "vendor_operator")
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	require.NoError(t, svc.RespondToComplaint(ctx, c.ID, vendor, vendorOp, "vendor reply here"))

	require.NoError(t, svc.EmployeeResolveComplaint(ctx, c.ID, user))
	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusResolved, got.Status)
}

func TestService_EmployeeResolveComplaint_CannotResolveEscalated(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	err := svc.EmployeeResolveComplaint(ctx, c.ID, user)
	assert.ErrorIs(t, err, feedback.ErrInvalidTransition)
}

func TestService_AdminResolveComplaint_Happy(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	require.NoError(t, svc.AdminResolveComplaint(ctx, c.ID, admin, "welfare committee resolved this", false))
	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusResolved, got.Status)
	require.NotNil(t, got.ResolvedBy)
	assert.Equal(t, admin, *got.ResolvedBy)
}

func TestService_AdminResolveComplaint_NotEscalated(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	err := svc.AdminResolveComplaint(ctx, c.ID, admin, "trying to resolve early", false)
	assert.ErrorIs(t, err, feedback.ErrInvalidTransition)
}

func TestService_AdminResolveComplaint_ShortResolution(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	err := svc.AdminResolveComplaint(ctx, c.ID, admin, "no", false)
	assert.ErrorIs(t, err, feedback.ErrValidation)
}

// recordingReverser captures every ReverseOrder call so tests can assert the
// payroll-reversal hook fires (or doesn't) for AdminResolveComplaint.
type recordingReverser struct {
	calls []string
}

func (r *recordingReverser) ReverseOrder(_ context.Context, orderID string) error {
	r.calls = append(r.calls, orderID)
	return nil
}

func TestService_AdminResolveComplaint_CompensateFalse_DoesNotReverse(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	rev := &recordingReverser{}
	svc.Reverser = rev

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	require.NoError(t, svc.AdminResolveComplaint(ctx, c.ID, admin, "no compensation needed here", false))
	assert.Empty(t, rev.calls)

	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusResolved, got.Status)
}

func TestService_AdminResolveComplaint_CompensateTrue_ReversesOrder(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	rev := &recordingReverser{}
	svc.Reverser = rev

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	orderID := seedPickedUpOrder(t, pool, user, vendor)
	c := fileComplaint(t, ctx, svc, user, orderID)
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	require.NoError(t, svc.AdminResolveComplaint(ctx, c.ID, admin, "compensate the employee fully", true))
	require.Equal(t, []string{orderID}, rev.calls)

	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusResolved, got.Status)
}

func TestService_AdminResolveComplaint_CompensateTrue_NilReverser_Errors(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	// Service.Reverser left nil intentionally.

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	admin := seedAdmin(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))
	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))

	err := svc.AdminResolveComplaint(ctx, c.ID, admin, "compensate the employee fully", true)
	require.Error(t, err)

	got, err := svc.GetComplaint(ctx, c.ID)
	require.NoError(t, err)
	assert.Equal(t, feedback.StatusEscalated, got.Status, "complaint state must not change when reverser is missing")
}

// ---------- Queries ----------

func TestService_ListMyAndVendorComplaints(t *testing.T) {
	pool, svc, cleanup := setup(t)
	defer cleanup()
	ctx := context.Background()

	user := seedEmployee(t, pool)
	vendor := seedVendor(t, pool)
	c := fileComplaint(t, ctx, svc, user, seedPickedUpOrder(t, pool, user, vendor))

	mine, err := svc.ListMyComplaints(ctx, user)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, c.ID, mine[0].ID)

	inbox, err := svc.ListVendorComplaints(ctx, vendor, nil)
	require.NoError(t, err)
	require.Len(t, inbox, 1)

	escalated, err := svc.ListEscalatedComplaints(ctx)
	require.NoError(t, err)
	assert.Empty(t, escalated)

	backdateComplaint(t, pool, c.ID, 25*time.Hour)
	require.NoError(t, svc.EscalateComplaint(ctx, c.ID, user))
	escalated, err = svc.ListEscalatedComplaints(ctx)
	require.NoError(t, err)
	require.Len(t, escalated, 1)
}
