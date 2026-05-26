package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/mcpserver"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	"github.com/takalawang/corporate-catering-system/services/api/internal/settlement"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// ---------------------------------------------------------------------------
// shared audit-path fakes (mirrors settlement/http/handlers_test.go precedent)
// ---------------------------------------------------------------------------

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the audit fake ignores the tx so auditAfter runs DB-free.
type fakeBeginner struct{ beginErr error }

func (b fakeBeginner) Begin(context.Context) (pgx.Tx, error) {
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return fakeTx{}, nil
}

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeAuditTx records each WriteTx call so tests can assert the audit row
// attribution (action / target). Satisfies mcpserver.AuditTx.
type fakeAuditTx struct {
	calls []auditCall
	err   error
}

type auditCall struct {
	action     string
	targetKind string
	targetID   string
}

func (a *fakeAuditTx) WriteTx(_ context.Context, _ pgx.Tx, _, _ *string, action, targetKind, targetID string, _ map[string]any, _ string) error {
	a.calls = append(a.calls, auditCall{action, targetKind, targetID})
	return a.err
}

// hasAction reports whether an audit row with the given action was written.
// Several domain services share the same fakeAuditTx (they also append their
// own audit row inside the service transaction), so callers assert presence of
// the MCP-attributed row ("mcp.<tool>") rather than an exact call count.
func (a *fakeAuditTx) hasAction(action string) bool {
	for _, c := range a.calls {
		if c.action == action {
			return true
		}
	}
	return false
}

// auditDeps returns a fakeBeginner + fakeAuditTx pair so the auditAfter write
// path is exercised on successful handler calls.
func auditDeps() (*fakeAuditTx, fakeBeginner) {
	return &fakeAuditTx{}, fakeBeginner{}
}

// ---------------------------------------------------------------------------
// ctx helpers
// ---------------------------------------------------------------------------

func adminCtx() context.Context {
	u := &identity.User{
		ID:           "admin-1",
		PrimaryEmail: "a@tbite.test",
		Role:         identity.RoleWelfareAdmin,
		Status:       identity.StatusActive,
	}
	return idhttp.ContextWithUser(context.Background(), u)
}

func vendorOpCtx() context.Context {
	u := &identity.User{
		ID:     "vop-1",
		Role:   identity.RoleVendorOperator,
		Status: identity.StatusActive,
	}
	return idhttp.ContextWithUser(context.Background(), u)
}

// ---------------------------------------------------------------------------
// menu fakes
// ---------------------------------------------------------------------------

type fakeItemRepo struct {
	rows   []*menu.ActiveItemRow
	err    error
	byID   map[string]*menu.Item // backs GetByID for the order Place/Modify paths
	getErr error
}

func (f *fakeItemRepo) Create(context.Context, *menu.Item) error { return nil }
func (f *fakeItemRepo) Update(context.Context, *menu.Item) error { return nil }
func (f *fakeItemRepo) SetStatus(context.Context, string, menu.ItemStatus) error {
	return nil
}
func (f *fakeItemRepo) GetByID(_ context.Context, id string) (*menu.Item, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if it, ok := f.byID[id]; ok {
		return it, nil
	}
	return &menu.Item{ID: id, VendorID: "v-1", PriceMinor: 12000}, nil
}
func (f *fakeItemRepo) ListByVendor(context.Context, string, bool) ([]*menu.MerchantItemRow, error) {
	return nil, nil
}
func (f *fakeItemRepo) ListActiveByPlant(context.Context, menu.EmployeeMenuFilter) ([]*menu.ActiveItemRow, error) {
	return f.rows, f.err
}

type fakeImageRepo struct{}

func (fakeImageRepo) Add(context.Context, *menu.Image) error { return nil }
func (fakeImageRepo) Remove(context.Context, string) error   { return nil }
func (fakeImageRepo) ListByItem(context.Context, string) ([]*menu.Image, error) {
	return nil, nil
}
func (fakeImageRepo) ReplaceForItem(context.Context, string, []string) error { return nil }

func menuServiceWith(rows []*menu.ActiveItemRow, err error) *menu.Service {
	return &menu.Service{
		Items:  &fakeItemRepo{rows: rows, err: err},
		Images: fakeImageRepo{},
	}
}

func seedMenuRow(id, name string, price int64) *menu.ActiveItemRow {
	return &menu.ActiveItemRow{
		Item: menu.Item{
			ID:         id,
			VendorID:   "v-1",
			Name:       name,
			PriceMinor: price,
			Tags:       []string{"vegan"},
		},
		VendorName:   "Sushi Place",
		Capacity:     10,
		Remain:       5,
		PickupWindow: "12:00-13:00",
		ETALabel:     "noon",
	}
}

// ---------------------------------------------------------------------------
// vendor fakes
// ---------------------------------------------------------------------------

type fakeVendorRepo struct {
	vendors  []*vendor.Vendor
	listErr  error
	getByID  map[string]*vendor.Vendor
	getErr   error
	statuses []vendor.Status
}

func (f *fakeVendorRepo) GetByID(_ context.Context, id string) (*vendor.Vendor, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if v, ok := f.getByID[id]; ok {
		return v, nil
	}
	return nil, errors.New("vendor not found")
}
func (f *fakeVendorRepo) GetByEmail(context.Context, string) (*vendor.Vendor, error) {
	return nil, nil
}
func (f *fakeVendorRepo) Create(context.Context, *vendor.Vendor) error { return nil }
func (f *fakeVendorRepo) UpdateStatus(context.Context, string, vendor.Status, *string) error {
	return nil
}
func (f *fakeVendorRepo) UpdateSettings(context.Context, string, int, int) error { return nil }
func (f *fakeVendorRepo) List(_ context.Context, statuses []vendor.Status) ([]*vendor.Vendor, error) {
	f.statuses = statuses
	return f.vendors, f.listErr
}

type fakePlantRepo struct {
	plants map[string][]string // vendorID -> plant codes
	err    error
}

func (f *fakePlantRepo) ListByVendor(_ context.Context, vendorID string) ([]*vendor.PlantMapping, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := []*vendor.PlantMapping{}
	for _, p := range f.plants[vendorID] {
		out = append(out, &vendor.PlantMapping{VendorID: vendorID, Plant: p, Active: true})
	}
	return out, nil
}
func (fakePlantRepo) ListVendorsForPlant(context.Context, string) ([]string, error) {
	return nil, nil
}
func (fakePlantRepo) Set(context.Context, string, []string) error             { return nil }
func (fakePlantRepo) SetWindow(context.Context, string, string, string) error { return nil }

type fakeOperatorRepo struct{}

func (fakeOperatorRepo) Get(context.Context, string, string) (*vendor.OperatorAccount, error) {
	return nil, nil
}
func (fakeOperatorRepo) ListByVendor(context.Context, string) ([]*vendor.OperatorAccount, error) {
	return nil, nil
}
func (fakeOperatorRepo) ListByVendorStatus(context.Context, string, []vendor.OperatorStatus) ([]*vendor.OperatorAccount, error) {
	return nil, nil
}
func (fakeOperatorRepo) Upsert(context.Context, *vendor.OperatorAccount) error { return nil }
func (fakeOperatorRepo) SetStatus(context.Context, string, string, vendor.OperatorStatus) error {
	return nil
}
func (fakeOperatorRepo) SetStatuses(context.Context, string, []vendor.OperatorStatus, vendor.OperatorStatus) error {
	return nil
}

// ---------------------------------------------------------------------------
// order fakes
// ---------------------------------------------------------------------------

type fakeOrderRepo struct {
	byUser  []*order.Order
	byID    map[string]*order.Order
	listErr error
	getErr  error
}

func (f *fakeOrderRepo) Create(context.Context, *order.Order) error { return nil }
func (f *fakeOrderRepo) GetByID(_ context.Context, id string) (*order.Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if o, ok := f.byID[id]; ok {
		return o, nil
	}
	return nil, errors.New("order not found")
}
func (f *fakeOrderRepo) UpdateStatus(context.Context, string, order.Status, order.Status, *string, *string, string) error {
	return nil
}
func (f *fakeOrderRepo) ListByUser(_ context.Context, _ string, _ time.Time) ([]*order.Order, error) {
	return f.byUser, f.listErr
}
func (f *fakeOrderRepo) ListPlacedDueForCutoff(context.Context, time.Time) ([]*order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) ListReadyOlderThan(context.Context, time.Time) ([]*order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) ListByVendorDay(context.Context, string, time.Time, []order.Status) ([]*order.Order, error) {
	return nil, nil
}
func (f *fakeOrderRepo) ListPickedOrNoShowInPeriod(context.Context, time.Time, time.Time) ([]*order.Order, error) {
	return nil, nil
}

type stubClock struct{ now time.Time }

func (c stubClock) Now() time.Time { return c.now }

func orderServiceWith(repo *fakeOrderRepo) *order.Service {
	return &order.Service{Orders: repo, Clock: stubClock{now: time.Now()}}
}

func seedOrder(id, userID string) *order.Order {
	return &order.Order{
		ID:         id,
		UserID:     userID,
		VendorID:   "v-1",
		Plant:      "F12B-3F",
		Status:     order.StatusPlaced,
		SupplyDate: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
		Items:      []order.Item{{ID: "i-1", MenuItemID: "m-1", Qty: 2}},
	}
}

// ---------------------------------------------------------------------------
// payroll fakes
// ---------------------------------------------------------------------------

type fakeBatchRepo struct {
	batches []*payroll.Batch
	byID    map[string]*payroll.Batch
	listErr error
}

func (f *fakeBatchRepo) Create(context.Context, *payroll.Batch) error           { return nil }
func (f *fakeBatchRepo) CreateTx(context.Context, pgx.Tx, *payroll.Batch) error { return nil }
func (f *fakeBatchRepo) GetByID(_ context.Context, id string) (*payroll.Batch, error) {
	if b, ok := f.byID[id]; ok {
		return b, nil
	}
	return nil, payroll.ErrBatchNotFound
}
func (f *fakeBatchRepo) GetByPeriod(context.Context, time.Time, time.Time) (*payroll.Batch, error) {
	return nil, payroll.ErrBatchNotFound
}
func (f *fakeBatchRepo) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.BatchStatus, payroll.BatchStatus, *string) error {
	return nil
}
func (f *fakeBatchRepo) SetExportInfoTx(context.Context, pgx.Tx, string, string, time.Time) error {
	return nil
}
func (f *fakeBatchRepo) List(_ context.Context, _ []payroll.BatchStatus) ([]*payroll.Batch, error) {
	return f.batches, f.listErr
}

type fakeDisputeRepo struct {
	byID map[string]*payroll.Dispute
}

func (f *fakeDisputeRepo) Create(context.Context, *payroll.Dispute) error { return nil }
func (f *fakeDisputeRepo) GetByID(_ context.Context, id string) (*payroll.Dispute, error) {
	if d, ok := f.byID[id]; ok {
		return d, nil
	}
	return nil, errors.New("dispute not found")
}
func (f *fakeDisputeRepo) UpdateStatusTx(context.Context, pgx.Tx, string, payroll.DisputeStatus, *string, string, int64) error {
	return nil
}
func (f *fakeDisputeRepo) ListByStatus(context.Context, []payroll.DisputeStatus) ([]*payroll.Dispute, error) {
	return nil, nil
}
func (f *fakeDisputeRepo) ListByUser(context.Context, string) ([]*payroll.Dispute, error) {
	return nil, nil
}

// payrollListService is enough to back payroll.list_batches + lock + resolve.
func payrollService(batches *fakeBatchRepo, disputes *fakeDisputeRepo, audit *fakeAuditTx, pool fakeBeginner) *payroll.Service {
	return &payroll.Service{
		Pool:     payrollPool{pool},
		Batches:  batches,
		Disputes: disputes,
		Audit:    audit,
		Outbox:   noopOutbox{},
		OrderTx:  noopOrderTx{},
		Clock:    stubClock{now: time.Now()},
	}
}

// payrollPool adapts fakeBeginner to payroll.txBeginner (which also needs Query).
type payrollPool struct{ fakeBeginner }

func (payrollPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }

type noopOutbox struct{}

func (noopOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

type noopOrderTx struct{}

func (noopOrderTx) UpdateStatusTx(context.Context, pgx.Tx, string, order.Status, order.Status) error {
	return nil
}

// ---------------------------------------------------------------------------
// compliance fakes
// ---------------------------------------------------------------------------

type fakeDocRepo struct {
	docs    []*compliance.Document
	listErr error
}

func (f *fakeDocRepo) Create(context.Context, *compliance.Document) error           { return nil }
func (f *fakeDocRepo) CreateTx(context.Context, pgx.Tx, *compliance.Document) error { return nil }
func (f *fakeDocRepo) GetByID(context.Context, string) (*compliance.Document, error) {
	return nil, nil
}
func (f *fakeDocRepo) ListByVendor(_ context.Context, _ string, _ bool) ([]*compliance.Document, error) {
	return f.docs, f.listErr
}
func (f *fakeDocRepo) UpdateStatus(context.Context, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (f *fakeDocRepo) UpdateStatusTx(context.Context, pgx.Tx, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (f *fakeDocRepo) ListExpiringBefore(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}
func (f *fakeDocRepo) ListPastExpiry(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}

type fakeAnomalyRepo struct {
	items   []*compliance.Anomaly
	byID    map[string]*compliance.Anomaly
	listErr error
}

func (f *fakeAnomalyRepo) Open(context.Context, *compliance.Anomaly) error { return nil }
func (f *fakeAnomalyRepo) GetByID(_ context.Context, id string) (*compliance.Anomaly, error) {
	if a, ok := f.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("anomaly not found")
}
func (f *fakeAnomalyRepo) List(_ context.Context, _ []compliance.AnomalyStatus, _ []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	return f.items, f.listErr
}
func (f *fakeAnomalyRepo) Triage(context.Context, string, string, string) error { return nil }
func (f *fakeAnomalyRepo) TriageTx(context.Context, pgx.Tx, string, string, string) error {
	return nil
}
func (f *fakeAnomalyRepo) Close(context.Context, string, string, string) error { return nil }
func (f *fakeAnomalyRepo) CloseTx(context.Context, pgx.Tx, string, string, string) error {
	return nil
}

type fakeAuditQuery struct {
	rows []compliance.AuditRow
	err  error
}

func (f *fakeAuditQuery) List(_ context.Context, _ compliance.AuditFilter) ([]compliance.AuditRow, error) {
	return f.rows, f.err
}

func complianceService(docs *fakeDocRepo, anom *fakeAnomalyRepo, audq *fakeAuditQuery, audit *fakeAuditTx, pool fakeBeginner) *compliance.Service {
	return &compliance.Service{
		Pool:     pool,
		Docs:     docs,
		Anomaly:  anom,
		AuditQry: audq,
		Audit:    audit,
		Outbox:   noopOutbox{},
	}
}

// ---------------------------------------------------------------------------
// feedback fakes
// ---------------------------------------------------------------------------

type fakeFeedbackOrders struct {
	info map[string]*feedback.OrderInfo
	err  error
}

func (f *fakeFeedbackOrders) GetOrderInfo(_ context.Context, id string) (*feedback.OrderInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	if o, ok := f.info[id]; ok {
		return o, nil
	}
	return nil, errors.New("order not found")
}

type fakeRatingRepo struct{ existing *feedback.Rating }

func (f *fakeRatingRepo) CreateTx(_ context.Context, _ pgx.Tx, r *feedback.Rating) error {
	r.ID = "rating-1"
	return nil
}
func (f *fakeRatingRepo) GetByOrder(context.Context, string) (*feedback.Rating, error) {
	if f.existing != nil {
		return f.existing, nil
	}
	return nil, feedback.ErrRatingNotFound
}
func (f *fakeRatingRepo) AggregateByVendorSince(context.Context, time.Time) ([]feedback.VendorRatingStat, error) {
	return nil, nil
}

type fakeComplaintRepo struct{}

func (f *fakeComplaintRepo) CreateTx(_ context.Context, _ pgx.Tx, c *feedback.Complaint) error {
	c.ID = "complaint-1"
	return nil
}
func (f *fakeComplaintRepo) GetByID(context.Context, string) (*feedback.Complaint, error) {
	return nil, nil
}
func (f *fakeComplaintRepo) UpdateStatusTx(context.Context, pgx.Tx, string, feedback.ComplaintStatus, feedback.ComplaintStatus, feedback.ComplaintUpdate) error {
	return nil
}
func (f *fakeComplaintRepo) ListByUser(context.Context, string) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (f *fakeComplaintRepo) ListByVendor(context.Context, string, []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (f *fakeComplaintRepo) ListByStatus(context.Context, []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	return nil, nil
}
func (f *fakeComplaintRepo) CountByVendorSince(context.Context, time.Time) ([]feedback.VendorComplaintStat, error) {
	return nil, nil
}

func feedbackService(orders *fakeFeedbackOrders, ratings *fakeRatingRepo, complaints *fakeComplaintRepo, audit *fakeAuditTx, pool fakeBeginner) *feedback.Service {
	return &feedback.Service{
		Pool:       pool,
		Orders:     orders,
		Ratings:    ratings,
		Complaints: complaints,
		Audit:      audit,
		Clock:      stubClock{now: time.Now()},
	}
}

// ---------------------------------------------------------------------------
// settlement fakes
// ---------------------------------------------------------------------------

type fakeSettlementRepo struct{}

func (fakeSettlementRepo) CreateTx(context.Context, pgx.Tx, *settlement.Settlement) error { return nil }
func (fakeSettlementRepo) GetByID(context.Context, string) (*settlement.Settlement, error) {
	return nil, nil
}
func (fakeSettlementRepo) ListByVendor(context.Context, string) ([]*settlement.Settlement, error) {
	return nil, nil
}
func (fakeSettlementRepo) ListByPeriod(context.Context, time.Time, time.Time) ([]*settlement.Settlement, error) {
	return nil, nil
}
func (fakeSettlementRepo) VoidTx(context.Context, pgx.Tx, string) error { return nil }

type fakeAggRepo struct {
	aggs []*settlement.VendorAggregate
	err  error
}

func (f *fakeAggRepo) AggregateByVendor(context.Context, time.Time, time.Time) ([]*settlement.VendorAggregate, error) {
	return f.aggs, f.err
}
func (f *fakeAggRepo) AggregateForVendor(context.Context, string, time.Time, time.Time) (*settlement.VendorAggregate, error) {
	return nil, nil
}
func (f *fakeAggRepo) StatusBreakdownForVendor(context.Context, string, time.Time, time.Time) (settlement.StatusBreakdown, error) {
	return settlement.StatusBreakdown{}, nil
}
func (f *fakeAggRepo) OrderLinesByIDs(context.Context, []string) ([]*settlement.SettlementOrderLine, error) {
	return nil, nil
}

func settlementService(aggs []*settlement.VendorAggregate, aggErr error, audit *fakeAuditTx, pool fakeBeginner) *settlement.Service {
	return &settlement.Service{
		Pool:        pool,
		Settlements: fakeSettlementRepo{},
		Orders:      &fakeAggRepo{aggs: aggs, err: aggErr},
		Audit:       audit,
	}
}

// ===========================================================================
// MENU TOOLS
// ===========================================================================

func TestMenuListForDay_Success(t *testing.T) {
	audit, pool := auditDeps()
	deps := mcpserver.Deps{
		Menu:  menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-1", "Sushi", 12000)}, nil),
		Pool:  pool,
		Audit: audit,
	}
	srv := mcpserver.New(deps)
	resp := callTool(t, newEmployeeCtx(), srv, "menu.list_for_day", map[string]any{"supply_date": "2026-05-27"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
	assert.Equal(t, "F12B-3F", out["plant"])
}

func TestMenuListForDay_NotAuthenticated(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, context.Background(), srv, "menu.list_for_day", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "not authenticated")
}

func TestMenuListForDay_BadDate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.list_for_day", map[string]any{"supply_date": "nope"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid date")
}

func TestMenuListForDay_ServiceNotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.list_for_day", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "menu service not configured")
}

func TestMenuSearch_Success(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith([]*menu.ActiveItemRow{
		seedMenuRow("m-1", "Vegan Bowl", 12000),
		seedMenuRow("m-2", "Sushi", 18000),
	}, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.search", map[string]any{
		"query":     "bowl",
		"tags":      []any{"vegan"},
		"price_min": float64(100),
		"price_max": float64(200),
		"in_stock":  true,
		"sort":      "name",
		"limit":     float64(1),
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
}

func TestMenuSearch_ServiceNotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.search", map[string]any{"query": "x"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "menu service not configured")
}

func TestMenuSearch_ServiceError(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, errors.New("boom"))})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.search", map[string]any{"query": "x"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "search menu")
}

func TestMenuGetItem_Success(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-7", "Ramen", 9000)}, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.get_item", map[string]any{"item_id": "m-7"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "m-7", out["ID"])
}

func TestMenuGetItem_NotFound(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-7", "Ramen", 9000)}, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.get_item", map[string]any{"item_id": "missing"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "item not available")
}

func TestMenuGetItem_MissingItemID(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.get_item", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestVendorListOpen_Success(t *testing.T) {
	vendors := &fakeVendorRepo{vendors: []*vendor.Vendor{
		{ID: "v-1", DisplayName: "Sushi Place", Status: vendor.StatusApproved, CutoffHour: 10, PreorderWindowDays: 3},
		{ID: "v-2", DisplayName: "Other Plant Co", Status: vendor.StatusApproved},
	}}
	plants := &fakePlantRepo{plants: map[string][]string{
		"v-1": {"F12B-3F"},
		"v-2": {"OTHER"},
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: plants, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.list_open", map[string]any{})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"]) // only v-1 serves F12B-3F
}

func TestVendorListOpen_ServiceError(t *testing.T) {
	vendors := &fakeVendorRepo{listErr: errors.New("db down")}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.list_open", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "list vendors")
}

func TestVendorListOpen_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, vendorOpCtx(), srv, "vendor.list_open", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "cannot list vendors")
}

// ===========================================================================
// ORDER TOOLS
// ===========================================================================

func TestOrderListMine_Success(t *testing.T) {
	audit, pool := auditDeps()
	repo := &fakeOrderRepo{byUser: []*order.Order{seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo), Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "order.list_mine", map[string]any{})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
	require.Len(t, audit.calls, 1)
	assert.Equal(t, "mcp.order.list_mine", audit.calls[0].action)
}

func TestOrderListMine_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "order.list_mine", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "cannot list employee orders")
}

func TestOrderListMine_ServiceError(t *testing.T) {
	repo := &fakeOrderRepo{listErr: errors.New("db error")}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "order.list_mine", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "list orders")
}

func TestOrderGet_Success(t *testing.T) {
	repo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "order.get", map[string]any{"order_id": "o-1"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "o-1", out["ID"])
}

func TestOrderGet_NotOwner(t *testing.T) {
	repo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": seedOrder("o-1", "someone-else")}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "order.get", map[string]any{"order_id": "o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestOrderGet_MissingArg(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.get", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestOrderGet_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "order.get", map[string]any{"order_id": "o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

func TestOrderPlace_MissingArgs(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	// missing plant
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestOrderPlace_BadDate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{
		"plant":       "F12B-3F",
		"supply_date": "bad",
		"items":       []any{map[string]any{"menu_item_id": "m-1", "qty": float64(1)}},
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid supply_date")
}

func TestOrderPlace_EmptyItems(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{
		"plant":       "F12B-3F",
		"supply_date": "2026-05-27",
		"items":       []any{},
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "items required")
}

func TestOrderPlace_BadItemEntry(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{
		"plant":       "F12B-3F",
		"supply_date": "2026-05-27",
		"items":       []any{"not-an-object"},
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "must be an object")
}

func TestOrderPlace_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "order.place", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

func TestOrderCancel_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "order.cancel", map[string]any{"order_id": "o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

func TestOrderCancel_MissingArg(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.cancel", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestOrderCancel_ServiceError(t *testing.T) {
	repo := &fakeOrderRepo{getErr: errors.New("not found")}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "order.cancel", map[string]any{"order_id": "o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestOrderModify_EmptyItems(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.modify", map[string]any{
		"order_id": "o-1",
		"items":    []any{},
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "items required")
}

func TestOrderModify_BadItemEntry(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.modify", map[string]any{
		"order_id": "o-1",
		"items":    []any{42},
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "must be an object")
}

func TestOrderModify_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "order.modify", map[string]any{"order_id": "o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

func TestOrderModify_MissingArg(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.modify", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

// ===========================================================================
// VENDOR ADMIN TOOLS
// ===========================================================================

func TestVendorList_Success(t *testing.T) {
	audit, pool := auditDeps()
	vendors := &fakeVendorRepo{vendors: []*vendor.Vendor{{ID: "v-1", DisplayName: "X", Status: vendor.StatusApproved}}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "vendor.list", map[string]any{"status": "approved"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
	assert.Equal(t, []vendor.Status{vendor.StatusApproved}, vendors.statuses)
	require.Len(t, audit.calls, 1)
}

func TestVendorList_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.list", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestVendorList_ServiceNotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "vendor.list", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "vendor service not configured")
}

func TestVendorSuspend_Success(t *testing.T) {
	audit, pool := auditDeps()
	vendors := &fakeVendorRepo{getByID: map[string]*vendor.Vendor{
		"v-1": {ID: "v-1", Status: vendor.StatusApproved},
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "vendor.suspend", map[string]any{"vendor_id": "v-1", "reason": "late"})
	assert.Contains(t, toolText(t, resp), "suspended")
	require.Len(t, audit.calls, 1)
	assert.Equal(t, "mcp.vendor.suspend", audit.calls[0].action)
}

func TestVendorSuspend_ServiceError(t *testing.T) {
	vendors := &fakeVendorRepo{getByID: map[string]*vendor.Vendor{
		"v-1": {ID: "v-1", Status: vendor.StatusSuspended}, // already suspended → ErrInvalidStatus
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, adminCtx(), srv, "vendor.suspend", map[string]any{"vendor_id": "v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestVendorSuspend_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.suspend", map[string]any{"vendor_id": "v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestVendorSuspend_MissingArg(t *testing.T) {
	svc := &vendor.Service{Vendors: &fakeVendorRepo{}, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, adminCtx(), srv, "vendor.suspend", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestVendorReinstate_Success(t *testing.T) {
	audit, pool := auditDeps()
	vendors := &fakeVendorRepo{getByID: map[string]*vendor.Vendor{
		"v-1": {ID: "v-1", Status: vendor.StatusSuspended},
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "vendor.reinstate", map[string]any{"vendor_id": "v-1"})
	assert.Contains(t, toolText(t, resp), "reinstated")
	require.Len(t, audit.calls, 1)
}

func TestVendorReinstate_ServiceError(t *testing.T) {
	vendors := &fakeVendorRepo{getByID: map[string]*vendor.Vendor{
		"v-1": {ID: "v-1", Status: vendor.StatusApproved}, // not suspended → ErrInvalidStatus
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, adminCtx(), srv, "vendor.reinstate", map[string]any{"vendor_id": "v-1"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestVendorReinstate_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.reinstate", map[string]any{"vendor_id": "v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

// ===========================================================================
// PAYROLL TOOLS
// ===========================================================================

func TestPayrollListBatches_Success(t *testing.T) {
	audit, pool := auditDeps()
	batches := &fakeBatchRepo{batches: []*payroll.Batch{{ID: "b-1", Status: payroll.BatchStatusDraft}}}
	svc := payrollService(batches, &fakeDisputeRepo{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "payroll.list_batches", map[string]any{"status": "draft"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
}

func TestPayrollListBatches_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "payroll.list_batches", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestPayrollListBatches_NotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "payroll.list_batches", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "payroll service not configured")
}

func TestPayrollLockBatch_Success(t *testing.T) {
	audit, pool := auditDeps()
	batches := &fakeBatchRepo{byID: map[string]*payroll.Batch{
		"b-1": {ID: "b-1", Status: payroll.BatchStatusDraft, PeriodStart: time.Now(), PeriodEnd: time.Now()},
	}}
	svc := payrollService(batches, &fakeDisputeRepo{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "payroll.lock_batch", map[string]any{"batch_id": "b-1"})
	assert.Contains(t, toolText(t, resp), "locked")
	assert.True(t, audit.hasAction("mcp.payroll.lock_batch"))
}

func TestPayrollLockBatch_ServiceError(t *testing.T) {
	batches := &fakeBatchRepo{byID: map[string]*payroll.Batch{
		"b-1": {ID: "b-1", Status: payroll.BatchStatusLocked}, // already locked
	}}
	svc := payrollService(batches, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.lock_batch", map[string]any{"batch_id": "b-1"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestPayrollLockBatch_MissingArg(t *testing.T) {
	svc := payrollService(&fakeBatchRepo{}, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.lock_batch", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestPayrollResolveDispute_Success(t *testing.T) {
	audit, pool := auditDeps()
	disputes := &fakeDisputeRepo{byID: map[string]*payroll.Dispute{
		"d-1": {ID: "d-1", OrderID: "o-1", EntryID: "e-1", Status: payroll.DisputeStatusOpen},
	}}
	svc := payrollService(&fakeBatchRepo{}, disputes, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "payroll.resolve_dispute", map[string]any{
		"dispute_id": "d-1",
		"status":     "resolved_reject",
		"resolution": "no merit",
	})
	assert.Contains(t, toolText(t, resp), "resolved")
	assert.True(t, audit.hasAction("mcp.payroll.resolve_dispute"))
}

func TestPayrollResolveDispute_InvalidStatus(t *testing.T) {
	svc := payrollService(&fakeBatchRepo{}, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.resolve_dispute", map[string]any{
		"dispute_id": "d-1",
		"status":     "garbage",
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "resolved_refund | resolved_reject")
}

func TestPayrollResolveDispute_MissingArgs(t *testing.T) {
	svc := payrollService(&fakeBatchRepo{}, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.resolve_dispute", map[string]any{"dispute_id": "d-1"})
	assert.True(t, toolResult(t, resp).IsError) // missing status
}

func TestPayrollResolveDispute_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "payroll.resolve_dispute", map[string]any{"dispute_id": "d-1", "status": "resolved_reject"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

// ===========================================================================
// AUDIT TOOL
// ===========================================================================

func TestAuditQuery_Success(t *testing.T) {
	audit, pool := auditDeps()
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{rows: []compliance.AuditRow{{ID: 1, Action: "x"}}}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "audit.query", map[string]any{
		"target_kind": "order",
		"target_id":   "o-1",
		"since":       "2026-01-01T00:00:00Z",
		"limit":       float64(10),
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
}

func TestAuditQuery_BadSince(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "audit.query", map[string]any{"since": "not-a-time"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "RFC3339")
}

func TestAuditQuery_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "audit.query", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestAuditQuery_NotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "audit.query", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "compliance service not configured")
}

// ===========================================================================
// FEEDBACK TOOLS
// ===========================================================================

func TestFeedbackRateOrder_Success(t *testing.T) {
	audit, pool := auditDeps()
	orders := &fakeFeedbackOrders{info: map[string]*feedback.OrderInfo{
		"o-1": {ID: "o-1", UserID: "user-1", VendorID: "v-1", Status: "picked_up"},
	}}
	svc := feedbackService(orders, &fakeRatingRepo{}, &fakeComplaintRepo{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc, Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.rate_order", map[string]any{
		"order_id": "o-1",
		"score":    float64(5),
		"comment":  "great",
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "rating-1", out["ID"])
	assert.True(t, audit.hasAction("mcp.feedback.rate_order"))
}

func TestFeedbackRateOrder_ValidationError(t *testing.T) {
	orders := &fakeFeedbackOrders{info: map[string]*feedback.OrderInfo{"o-1": {ID: "o-1", UserID: "user-1", Status: "picked_up"}}}
	svc := feedbackService(orders, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.rate_order", map[string]any{
		"order_id": "o-1",
		"score":    float64(9), // out of range
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestFeedbackRateOrder_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "feedback.rate_order", map[string]any{"order_id": "o-1", "score": float64(5)})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

func TestFeedbackRateOrder_NotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.rate_order", map[string]any{"order_id": "o-1", "score": float64(5)})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "feedback service not configured")
}

func TestFeedbackRateOrder_MissingArg(t *testing.T) {
	svc := feedbackService(&fakeFeedbackOrders{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.rate_order", map[string]any{"score": float64(5)})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestFeedbackFileComplaint_Success(t *testing.T) {
	audit, pool := auditDeps()
	orders := &fakeFeedbackOrders{info: map[string]*feedback.OrderInfo{
		"o-1": {ID: "o-1", UserID: "user-1", VendorID: "v-1", Status: "picked_up"},
	}}
	svc := feedbackService(orders, &fakeRatingRepo{}, &fakeComplaintRepo{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc, Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{
		"order_id":    "o-1",
		"category":    "quality",
		"description": "cold food and late",
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "complaint-1", out["ID"])
	assert.True(t, audit.hasAction("mcp.feedback.file_complaint"))
}

func TestFeedbackFileComplaint_ValidationError(t *testing.T) {
	orders := &fakeFeedbackOrders{info: map[string]*feedback.OrderInfo{"o-1": {ID: "o-1", UserID: "user-1", Status: "picked_up"}}}
	svc := feedbackService(orders, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{
		"order_id":    "o-1",
		"category":    "not-a-category",
		"description": "too short ok actually fine",
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestFeedbackFileComplaint_MissingArgs(t *testing.T) {
	svc := feedbackService(&fakeFeedbackOrders{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	// missing category + description
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{"order_id": "o-1"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestFeedbackFileComplaint_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "feedback.file_complaint", map[string]any{"order_id": "o-1", "category": "quality", "description": "xxxxxx"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employee")
}

// ===========================================================================
// SETTLEMENT TOOL
// ===========================================================================

func TestSettlementClosePeriod_Success(t *testing.T) {
	audit, pool := auditDeps()
	aggs := []*settlement.VendorAggregate{{VendorID: "v-1", OrderCount: 3, GrossMinor: 30000, OrderIDs: []string{"o-1"}}}
	svc := settlementService(aggs, nil, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Settlement: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{
		"period_start": "2026-05-01",
		"period_end":   "2026-05-31",
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
	assert.True(t, audit.hasAction("mcp.settlement.close_period"))
}

func TestSettlementClosePeriod_BadStart(t *testing.T) {
	svc := settlementService(nil, nil, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Settlement: svc})
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{
		"period_start": "bad",
		"period_end":   "2026-05-31",
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid period_start")
}

func TestSettlementClosePeriod_BadEnd(t *testing.T) {
	svc := settlementService(nil, nil, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Settlement: svc})
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{
		"period_start": "2026-05-01",
		"period_end":   "bad",
	})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid period_end")
}

func TestSettlementClosePeriod_ServiceError(t *testing.T) {
	svc := settlementService(nil, errors.New("agg failed"), &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Settlement: svc})
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{
		"period_start": "2026-05-01",
		"period_end":   "2026-05-31",
	})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestSettlementClosePeriod_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "settlement.close_period", map[string]any{"period_start": "2026-05-01", "period_end": "2026-05-31"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestSettlementClosePeriod_MissingArgs(t *testing.T) {
	svc := settlementService(nil, nil, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Settlement: svc})
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{"period_start": "2026-05-01"})
	assert.True(t, toolResult(t, resp).IsError)
}

// ===========================================================================
// COMPLIANCE TOOLS
// ===========================================================================

func TestDocumentList_Success(t *testing.T) {
	audit, pool := auditDeps()
	svc := complianceService(&fakeDocRepo{docs: []*compliance.Document{{ID: "doc-1"}}}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "document.list", map[string]any{"vendor_id": "v-1", "include_all": true})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
}

func TestDocumentList_ServiceError(t *testing.T) {
	svc := complianceService(&fakeDocRepo{listErr: errors.New("db")}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "document.list", map[string]any{"vendor_id": "v-1"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestDocumentList_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "document.list", map[string]any{"vendor_id": "v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestDocumentList_MissingArg(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "document.list", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestDocumentReview_Success(t *testing.T) {
	audit, pool := auditDeps()
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "document.review", map[string]any{
		"document_id": "doc-1",
		"status":      "approved",
		"notes":       "ok",
	})
	assert.Contains(t, toolText(t, resp), "reviewed")
	assert.True(t, audit.hasAction("mcp.document.review"))
}

func TestDocumentReview_InvalidStatus(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "document.review", map[string]any{"document_id": "doc-1", "status": "maybe"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestDocumentReview_MissingArgs(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "document.review", map[string]any{"document_id": "doc-1"})
	assert.True(t, toolResult(t, resp).IsError) // missing status
}

func TestDocumentReview_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "document.review", map[string]any{"document_id": "doc-1", "status": "approved"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestAnomalyList_Success(t *testing.T) {
	audit, pool := auditDeps()
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{items: []*compliance.Anomaly{{ID: "a-1"}}}, &fakeAuditQuery{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "anomaly.list", map[string]any{"status": "open", "severity": "high"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(1), out["count"])
}

func TestAnomalyList_ServiceError(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{listErr: errors.New("db")}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "anomaly.list", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestAnomalyList_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "anomaly.list", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestAnomalyTriage_Success(t *testing.T) {
	audit, pool := auditDeps()
	anom := &fakeAnomalyRepo{byID: map[string]*compliance.Anomaly{"a-1": {ID: "a-1", TargetKind: "vendor", TargetID: "v-1"}}}
	svc := complianceService(&fakeDocRepo{}, anom, &fakeAuditQuery{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "anomaly.triage", map[string]any{
		"anomaly_id": "a-1",
		"notes":      "looking",
		"action":     "warn",
	})
	assert.Contains(t, toolText(t, resp), "triaged")
	assert.True(t, audit.hasAction("mcp.anomaly.triage"))
}

func TestAnomalyTriage_ServiceError(t *testing.T) {
	// invalid action surfaces ErrInvalidAction from the service
	anom := &fakeAnomalyRepo{byID: map[string]*compliance.Anomaly{"a-1": {ID: "a-1"}}}
	svc := complianceService(&fakeDocRepo{}, anom, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "anomaly.triage", map[string]any{"anomaly_id": "a-1", "action": "explode"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestAnomalyTriage_MissingArg(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "anomaly.triage", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestAnomalyTriage_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "anomaly.triage", map[string]any{"anomaly_id": "a-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestAnomalyClose_Success(t *testing.T) {
	audit, pool := auditDeps()
	anom := &fakeAnomalyRepo{byID: map[string]*compliance.Anomaly{"a-1": {ID: "a-1"}}}
	svc := complianceService(&fakeDocRepo{}, anom, &fakeAuditQuery{}, audit, pool)
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc, Pool: pool, Audit: audit})
	resp := callTool(t, adminCtx(), srv, "anomaly.close", map[string]any{"anomaly_id": "a-1", "notes": "done"})
	assert.Contains(t, toolText(t, resp), "closed")
	assert.True(t, audit.hasAction("mcp.anomaly.close"))
}

func TestAnomalyClose_MissingArg(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "anomaly.close", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestAnomalyClose_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "anomaly.close", map[string]any{"anomaly_id": "a-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only welfare_admin")
}

func TestAnomalyClose_NotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, adminCtx(), srv, "anomaly.close", map[string]any{"anomaly_id": "a-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "compliance service not configured")
}

// ===========================================================================
// CHATGPT search / fetch tools
// ===========================================================================

func TestSearch_WithMenuAndOrders(t *testing.T) {
	menuSvc := menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-1", "Vegan Bowl", 12000)}, nil)
	orderRepo := &fakeOrderRepo{byUser: []*order.Order{seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc, Order: orderServiceWith(orderRepo)})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": ""})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestSearch_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, vendorOpCtx(), srv, "search", map[string]any{"query": "x"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "cannot search")
}

func TestFetch_Menu_Success(t *testing.T) {
	menuSvc := menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-1", "Vegan Bowl", 12000)}, nil)
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "menu:m-1"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "menu:m-1", out["id"])
}

func TestFetch_Menu_NotFound(t *testing.T) {
	menuSvc := menuServiceWith([]*menu.ActiveItemRow{seedMenuRow("m-1", "Vegan Bowl", 12000)}, nil)
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "menu:nope"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "menu item not found")
}

func TestFetch_Order_Success(t *testing.T) {
	orderRepo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(orderRepo)})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "order:o-1"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "order:o-1", out["id"])
}

func TestFetch_Order_NotEmployee(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, adminCtx(), srv, "fetch", map[string]any{"id": "order:o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "only employees")
}

func TestFetch_Vendor_Success(t *testing.T) {
	vendors := &fakeVendorRepo{getByID: map[string]*vendor.Vendor{
		"v-1": {ID: "v-1", DisplayName: "Sushi Place", Status: vendor.StatusApproved, CutoffHour: 10, PreorderWindowDays: 3},
	}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "vendor:v-1"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "vendor:v-1", out["id"])
}

func TestFetch_Vendor_NotFound(t *testing.T) {
	svc := &vendor.Service{Vendors: &fakeVendorRepo{getErr: errors.New("gone")}, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "vendor:v-9"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestFetch_Anonymous(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, context.Background(), srv, "fetch", map[string]any{"id": "menu:m-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "not authenticated")
}

func TestFetch_MissingID(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

// ---------------------------------------------------------------------------
// full order.Service wiring (covers order.place / cancel / modify success)
// ---------------------------------------------------------------------------

type fakeOrderTx struct{}

func (fakeOrderTx) CreateTx(_ context.Context, _ pgx.Tx, o *order.Order) error {
	o.ID = "new-order-1"
	return nil
}
func (fakeOrderTx) UpdateStatusTx(context.Context, pgx.Tx, string, order.Status, order.Status) error {
	return nil
}
func (fakeOrderTx) ReplaceItemsTx(context.Context, pgx.Tx, string, []order.Item, int64, string) error {
	return nil
}
func (fakeOrderTx) MarkReadyTx(context.Context, pgx.Tx, string) error    { return nil }
func (fakeOrderTx) MarkPickedUpTx(context.Context, pgx.Tx, string) error { return nil }
func (fakeOrderTx) MarkNoShowTx(context.Context, pgx.Tx, string) error   { return nil }

type fakeStateTx struct{}

func (fakeStateTx) AppendTx(context.Context, pgx.Tx, *order.StateEvent) error { return nil }

type fakeOrderOutbox struct{}

func (fakeOrderOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

type fakeQuotaTx struct{}

func (fakeQuotaTx) DecrementTx(context.Context, pgx.Tx, string, time.Time, int) (int, error) {
	return 5, nil
}
func (fakeQuotaTx) RestoreTx(context.Context, pgx.Tx, string, time.Time, int) error { return nil }

// fullOrderService wires every transactional collaborator with no-op fakes so
// the order.place / cancel / modify write closures run DB-free. The Clock is
// pinned well before the seed order's cutoff so cutoff checks pass.
func fullOrderService(repo *fakeOrderRepo) *order.Service {
	return &order.Service{
		Pool:     fakeBeginner{},
		Orders:   repo,
		OrdersTx: fakeOrderTx{},
		StateTx:  fakeStateTx{},
		OutboxTx: fakeOrderOutbox{},
		QuotaTx:  fakeQuotaTx{},
		AuditTx:  &fakeAuditTx{},
		Items:    &fakeItemRepo{},
		Plants:   &fakePlantRepo{plants: map[string][]string{"v-1": {"F12B-3F"}}},
		Vendors:  &fakeVendorRepo{getByID: map[string]*vendor.Vendor{"v-1": {ID: "v-1", Status: vendor.StatusApproved, CutoffHour: 23, PreorderWindowDays: 30}}},
		Clock:    stubClock{now: time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)},
	}
}

func TestOrderPlace_Success(t *testing.T) {
	audit, pool := auditDeps()
	repo := &fakeOrderRepo{}
	srv := mcpserver.New(mcpserver.Deps{Order: fullOrderService(repo), Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{
		"plant":       "F12B-3F",
		"supply_date": "2026-05-10",
		"items":       []any{map[string]any{"menu_item_id": "m-1", "qty": float64(2)}},
		"notes":       "no onions",
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "new-order-1", out["ID"])
	assert.True(t, audit.hasAction("mcp.order.place"))
}

func TestOrderPlace_ServiceError(t *testing.T) {
	// vendor does not serve the requested plant → ErrVendorPlantMismatch
	svc := fullOrderService(&fakeOrderRepo{})
	srv := mcpserver.New(mcpserver.Deps{Order: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{
		"plant":       "OTHER-PLANT",
		"supply_date": "2026-05-10",
		"items":       []any{map[string]any{"menu_item_id": "m-1", "qty": float64(2)}},
	})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestOrderCancel_Success(t *testing.T) {
	audit, pool := auditDeps()
	o := seedOrder("o-1", "user-1")
	o.SupplyDate = time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	repo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": o}}
	srv := mcpserver.New(mcpserver.Deps{Order: fullOrderService(repo), Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "order.cancel", map[string]any{"order_id": "o-1"})
	assert.Contains(t, toolText(t, resp), "cancelled")
	assert.True(t, audit.hasAction("mcp.order.cancel"))
}

func TestOrderModify_Success(t *testing.T) {
	audit, pool := auditDeps()
	o := seedOrder("o-1", "user-1")
	o.CutoffAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC) // after pinned clock
	repo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": o}}
	srv := mcpserver.New(mcpserver.Deps{Order: fullOrderService(repo), Pool: pool, Audit: audit})
	resp := callTool(t, newEmployeeCtx(), srv, "order.modify", map[string]any{
		"order_id": "o-1",
		"items":    []any{map[string]any{"menu_item_id": "m-1", "qty": float64(3)}},
		"notes":    "extra rice",
	})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "o-1", out["ID"])
	assert.True(t, audit.hasAction("mcp.order.modify"))
}

func TestOrderModify_ServiceError(t *testing.T) {
	// order not in PLACED state → ErrInvalidTransition
	o := seedOrder("o-1", "user-1")
	o.Status = order.StatusCancelled
	repo := &fakeOrderRepo{byID: map[string]*order.Order{"o-1": o}}
	srv := mcpserver.New(mcpserver.Deps{Order: fullOrderService(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "order.modify", map[string]any{
		"order_id": "o-1",
		"items":    []any{map[string]any{"menu_item_id": "m-1", "qty": float64(1)}},
	})
	assert.True(t, toolResult(t, resp).IsError)
}

// ---------------------------------------------------------------------------
// "service not configured" branches for write handlers (deps with nil service)
// ---------------------------------------------------------------------------

func TestWriteHandlers_ServiceNotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	cases := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"order.place", map[string]any{"plant": "F12B-3F", "supply_date": "2026-05-10", "items": []any{map[string]any{"menu_item_id": "m-1", "qty": float64(1)}}}, "order service not configured"},
		{"order.cancel", map[string]any{"order_id": "o-1"}, "order service not configured"},
		{"order.modify", map[string]any{"order_id": "o-1", "items": []any{map[string]any{"menu_item_id": "m-1", "qty": float64(1)}}}, "order service not configured"},
		{"vendor.suspend", map[string]any{"vendor_id": "v-1"}, "vendor service not configured"},
		{"vendor.reinstate", map[string]any{"vendor_id": "v-1"}, "vendor service not configured"},
		{"vendor.list_open", map[string]any{}, "vendor service not configured"},
		{"payroll.lock_batch", map[string]any{"batch_id": "b-1"}, "payroll service not configured"},
		{"payroll.resolve_dispute", map[string]any{"dispute_id": "d-1", "status": "resolved_reject"}, "payroll service not configured"},
		{"settlement.close_period", map[string]any{"period_start": "2026-05-01", "period_end": "2026-05-31"}, "settlement service not configured"},
		{"document.review", map[string]any{"document_id": "doc-1", "status": "approved"}, "compliance service not configured"},
		{"anomaly.triage", map[string]any{"anomaly_id": "a-1"}, "compliance service not configured"},
		{"feedback.file_complaint", map[string]any{"order_id": "o-1", "category": "quality", "description": "xxxxxx"}, "feedback service not configured"},
	}
	for _, tc := range cases {
		ctx := adminCtx()
		if tc.tool == "order.place" || tc.tool == "order.cancel" || tc.tool == "order.modify" ||
			tc.tool == "vendor.list_open" || tc.tool == "feedback.file_complaint" {
			ctx = newEmployeeCtx()
		}
		resp := callTool(t, ctx, srv, tc.tool, tc.args)
		res := toolResult(t, resp)
		assert.Truef(t, res.IsError, "%s should error", tc.tool)
		assert.Containsf(t, toolText(t, resp), tc.want, "%s", tc.tool)
	}
}

// ---------------------------------------------------------------------------
// "not authenticated" branches for the write handlers (anonymous ctx)
// ---------------------------------------------------------------------------

func TestWriteHandlers_Anonymous(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	tools := []string{
		"order.place", "order.cancel", "order.modify", "order.get", "order.list_mine",
		"vendor.list", "vendor.suspend", "vendor.reinstate", "vendor.list_open",
		"payroll.list_batches", "payroll.lock_batch", "payroll.resolve_dispute",
		"audit.query", "settlement.close_period",
		"document.list", "document.review", "anomaly.list", "anomaly.triage", "anomaly.close",
		"feedback.rate_order", "feedback.file_complaint",
		"menu.list_for_day", "menu.search", "menu.get_item",
	}
	for _, tool := range tools {
		resp := callTool(t, context.Background(), srv, tool, map[string]any{})
		res := toolResult(t, resp)
		assert.Truef(t, res.IsError, "%s should error", tool)
		assert.Containsf(t, toolText(t, resp), "not authenticated", "%s", tool)
	}
}

// ---------------------------------------------------------------------------
// chatgpt fetch / format-helper coverage (sold-out, full metadata, short id)
// ---------------------------------------------------------------------------

func seedRichMenuRow(id string) *menu.ActiveItemRow {
	return &menu.ActiveItemRow{
		Item: menu.Item{
			ID:          id,
			VendorID:    "v-1",
			Name:        "Deluxe Bento",
			Description: "A full set with miso soup",
			PriceMinor:  25000,
			Tags:        []string{"vegan", "low_carb"},
			Badges:      []string{"chef_special"},
		},
		VendorName:   "Bento Co",
		Capacity:     10,
		Remain:       0, // sold out
		SoldOut:      true,
		PickupWindow: "12:00-13:00",
		ETALabel:     "noon",
	}
}

func TestFetch_Menu_SoldOutFullMetadata(t *testing.T) {
	menuSvc := menuServiceWith([]*menu.ActiveItemRow{seedRichMenuRow("m-rich")}, nil)
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "menu:m-rich"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "menu:m-rich", out["id"])
	assert.Contains(t, out["text"], "SOLD OUT")
	assert.Contains(t, out["text"], "Tags:")
	assert.Contains(t, out["text"], "Badges:")
}

func TestSearch_SoldOutSnippet(t *testing.T) {
	menuSvc := menuServiceWith([]*menu.ActiveItemRow{seedRichMenuRow("m-rich")}, nil)
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": ""})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	require.Len(t, results, 1)
	first := results[0].(map[string]any)
	assert.Contains(t, first["text"], "sold out")
}

func TestFetch_Order_ShortIDFullUUID(t *testing.T) {
	// Long UUID exercises the shortID truncation branch.
	longID := "abcdef12-3456-7890-abcd-ef1234567890"
	o := seedOrder(longID, "user-1")
	repo := &fakeOrderRepo{byID: map[string]*order.Order{longID: o}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "order:" + longID})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "order:"+longID, out["id"])
	assert.Contains(t, out["title"], "abcdef12") // truncated to 8 chars
}

func TestFetch_Menu_NoPlant(t *testing.T) {
	menuSvc := menuServiceWith(nil, nil)
	u := &identity.User{ID: "u", Role: identity.RoleEmployee, Status: identity.StatusActive} // no plant
	ctx := idhttp.ContextWithUser(context.Background(), u)
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc})
	resp := callTool(t, ctx, srv, "fetch", map[string]any{"id": "menu:m-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "no home plant")
}

func TestFetch_Order_ServiceUnavailable(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{}) // no order service
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "order:o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "order service unavailable")
}

func TestFetch_Vendor_ServiceUnavailable(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{}) // no vendor service
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "vendor:v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "vendor service unavailable")
}

func TestMenuListForDay_ServiceError(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, errors.New("db down"))})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.list_for_day", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "list menu")
}
