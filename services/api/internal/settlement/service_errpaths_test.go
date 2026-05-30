package settlement_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"

	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
)

// These tests exercise the repository/transaction error branches of
// settlement.Service without a container by injecting fakes through the
// service's interface seams (txBeginner / SettlementRepository /
// OrderAggregateRepository / AuditTxWriter). They are pure in-process.

// --- fake pgx.Tx that begins fine and commits/rolls back as no-ops ---

type fakeTx struct{}

func (fakeTx) Begin(ctx context.Context) (pgx.Tx, error) { return fakeTx{}, nil }
func (fakeTx) Commit(ctx context.Context) error          { return nil }
func (fakeTx) Rollback(ctx context.Context) error        { return nil }
func (fakeTx) LargeObjects() pgx.LargeObjects             { return pgx.LargeObjects{} }
func (fakeTx) Conn() *pgx.Conn                            { return nil }
func (fakeTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return nil
}
func (fakeTx) CopyFrom(ctx context.Context, tn pgx.Identifier, cols []string, src pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (fakeTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }

// okBeginner hands BeginFunc a fakeTx so the close/void closure runs.
type okBeginner struct{}

func (okBeginner) Begin(ctx context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

// --- fake repositories ---

type fakeSettlementRepo struct {
	createErr   error
	getByID     *settlement.Settlement
	getByIDErr  error
	voidErr     error
	listVendErr error
}

func (r fakeSettlementRepo) CreateTx(ctx context.Context, tx pgx.Tx, s *settlement.Settlement) error {
	return r.createErr
}
func (r fakeSettlementRepo) GetByID(ctx context.Context, id string) (*settlement.Settlement, error) {
	return r.getByID, r.getByIDErr
}
func (r fakeSettlementRepo) ListByVendor(ctx context.Context, vendorID string) ([]*settlement.Settlement, error) {
	return nil, r.listVendErr
}
func (r fakeSettlementRepo) ListByPeriod(ctx context.Context, start, end time.Time) ([]*settlement.Settlement, error) {
	return nil, nil
}
func (r fakeSettlementRepo) VoidTx(ctx context.Context, tx pgx.Tx, id string) error {
	return r.voidErr
}

type fakeOrderRepo struct {
	aggByVendor    []*settlement.VendorAggregate
	aggByVendorErr error
	aggForVendor   *settlement.VendorAggregate
	aggForVendErr  error
	breakdownErr   error
	linesErr       error
}

func (r fakeOrderRepo) AggregateByVendor(ctx context.Context, start, end time.Time) ([]*settlement.VendorAggregate, error) {
	return r.aggByVendor, r.aggByVendorErr
}
func (r fakeOrderRepo) AggregateForVendor(ctx context.Context, vendorID string, start, end time.Time) (*settlement.VendorAggregate, error) {
	return r.aggForVendor, r.aggForVendErr
}
func (r fakeOrderRepo) StatusBreakdownForVendor(ctx context.Context, vendorID string, start, end time.Time) (settlement.StatusBreakdown, error) {
	return settlement.StatusBreakdown{}, r.breakdownErr
}
func (r fakeOrderRepo) OrderLinesByIDs(ctx context.Context, orderIDs []string) ([]*settlement.SettlementOrderLine, error) {
	return nil, r.linesErr
}

type noopAudit struct{}

func (noopAudit) WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error { return nil }

func aprPeriod() (time.Time, time.Time) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, -1)
}

// === CloseSettlement: AggregateByVendor error (service.go:51) ===

func TestService_CloseSettlement_AggregateError(t *testing.T) {
	want := errors.New("aggregate boom")
	svc := &settlement.Service{
		Pool:        okBeginner{},
		Settlements: fakeSettlementRepo{},
		Orders:      fakeOrderRepo{aggByVendorErr: want},
		Audit:       noopAudit{},
	}
	start, end := aprPeriod()
	_, err := svc.CloseSettlement(context.Background(), settlement.CloseSettlementInput{
		PeriodStart: start, PeriodEnd: end, ClosedBy: "admin",
	})
	assert.ErrorIs(t, err, want)
}

// === VoidSettlement: VoidTx error inside the tx closure (service.go:108) ===

func TestService_VoidSettlement_VoidTxError(t *testing.T) {
	want := errors.New("void boom")
	start, end := aprPeriod()
	closed := &settlement.Settlement{ID: "s1", VendorID: "v1", PeriodStart: start, PeriodEnd: end, Status: settlement.StatusClosed}
	svc := &settlement.Service{
		Pool:        okBeginner{},
		Settlements: fakeSettlementRepo{getByID: closed, voidErr: want},
		Orders:      fakeOrderRepo{},
		Audit:       noopAudit{},
	}
	err := svc.VoidSettlement(context.Background(), "s1", "admin")
	assert.ErrorIs(t, err, want)
}

// === Reconciliation: invalid period (service.go:125) ===

func TestService_Reconciliation_InvalidPeriod(t *testing.T) {
	svc := &settlement.Service{Orders: fakeOrderRepo{}}
	start, end := aprPeriod()
	_, err := svc.Reconciliation(context.Background(), "v1", end, start)
	assert.ErrorIs(t, err, settlement.ErrInvalidPeriod)
}

// === Reconciliation: AggregateForVendor error (service.go:130) ===

func TestService_Reconciliation_AggregateError(t *testing.T) {
	want := errors.New("agg boom")
	svc := &settlement.Service{Orders: fakeOrderRepo{aggForVendErr: want}}
	start, end := aprPeriod()
	_, err := svc.Reconciliation(context.Background(), "v1", start, end)
	assert.ErrorIs(t, err, want)
}

// === Reconciliation: StatusBreakdownForVendor error (service.go:134) ===

func TestService_Reconciliation_BreakdownError(t *testing.T) {
	want := errors.New("breakdown boom")
	svc := &settlement.Service{Orders: fakeOrderRepo{
		aggForVendor: &settlement.VendorAggregate{VendorID: "v1"},
		breakdownErr: want,
	}}
	start, end := aprPeriod()
	_, err := svc.Reconciliation(context.Background(), "v1", start, end)
	assert.ErrorIs(t, err, want)
}

// === GetVendorSettlement: GetByID error (service.go:157) ===

func TestService_GetVendorSettlement_GetByIDError(t *testing.T) {
	want := errors.New("getbyid boom")
	svc := &settlement.Service{
		Settlements: fakeSettlementRepo{getByIDErr: want},
		Orders:      fakeOrderRepo{},
	}
	_, _, err := svc.GetVendorSettlement(context.Background(), "v1", "s1")
	assert.ErrorIs(t, err, want)
}

// === GetVendorSettlement: OrderLinesByIDs error (service.go:164) ===

func TestService_GetVendorSettlement_OrderLinesError(t *testing.T) {
	want := errors.New("lines boom")
	st := &settlement.Settlement{ID: "s1", VendorID: "v1", OrderIDs: []string{"o1"}}
	svc := &settlement.Service{
		Settlements: fakeSettlementRepo{getByID: st},
		Orders:      fakeOrderRepo{linesErr: want},
	}
	_, _, err := svc.GetVendorSettlement(context.Background(), "v1", "s1")
	assert.ErrorIs(t, err, want)
}

// === ListSettlementsByPeriod: invalid period (service.go:172) ===

func TestService_ListSettlementsByPeriod_InvalidPeriod(t *testing.T) {
	svc := &settlement.Service{Settlements: fakeSettlementRepo{}}
	start, end := aprPeriod()
	_, err := svc.ListSettlementsByPeriod(context.Background(), end, start)
	assert.ErrorIs(t, err, settlement.ErrInvalidPeriod)
}
