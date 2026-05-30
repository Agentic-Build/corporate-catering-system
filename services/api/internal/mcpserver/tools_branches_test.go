package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/mcpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// === ChatGPT search/fetch uncovered branches ===

// searchMenuItems swallows a menu service error and returns the results so
// far unchanged (the connector must never crash on a backend hiccup).
func TestSearch_MenuServiceError(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, errors.New("db down"))})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": "x"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Empty(t, results)
}

// searchOrders swallows an order service error the same way.
func TestSearch_OrderServiceError(t *testing.T) {
	repo := &fakeOrderRepo{listErr: errors.New("db down")}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": "x"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Empty(t, results)
}

// searchOrders skips orders that don't match the (non-empty) query.
func TestSearch_OrderQueryNoMatch(t *testing.T) {
	repo := &fakeOrderRepo{byUser: []*order.Order{seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	// query that matches neither plant, supply_date, nor status of the seed order.
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": "zzz-no-match"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Empty(t, results)
}

// manyMenuRows builds n distinct menu rows so the 20-result cap is reached.
func manyMenuRows(n int) []*menu.ActiveItemRow {
	rows := make([]*menu.ActiveItemRow, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, seedMenuRow("m-"+itoa(i), "Item", 12000))
	}
	return rows
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// search caps menu results at 20 (the break inside searchMenuItems), and the
// >=20 guard at the top of searchOrders then short-circuits the order loop.
func TestSearch_CapsAtTwentyResults(t *testing.T) {
	menuSvc := menuServiceWith(manyMenuRows(25), nil)
	orderRepo := &fakeOrderRepo{byUser: []*order.Order{seedOrder("o-1", "user-1")}}
	srv := mcpserver.New(mcpserver.Deps{Menu: menuSvc, Order: orderServiceWith(orderRepo)})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": ""})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Len(t, results, 20)
}

// searchOrders breaks once 20 results accumulate while iterating orders (no
// menu items, 25 matching orders → the order loop hits its >=20 break).
func TestSearch_OrderLoopCapsAtTwenty(t *testing.T) {
	orders := make([]*order.Order, 0, 25)
	for i := 0; i < 25; i++ {
		orders = append(orders, seedOrder("o-"+itoa(i), "user-1"))
	}
	repo := &fakeOrderRepo{byUser: orders}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "search", map[string]any{"query": ""})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	results, _ := out["results"].([]any)
	assert.Len(t, results, 20)
}

// fetchMenuItem rejects a role that cannot read the menu.
func TestFetch_Menu_RoleGate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, nil)})
	resp := callTool(t, vendorOpCtx(), srv, "fetch", map[string]any{"id": "menu:m-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "role cannot read menu")
}

// fetchMenuItem surfaces a menu service error.
func TestFetch_Menu_ServiceError(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, errors.New("db down"))})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "menu:m-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "fetch menu")
}

// fetchOrder surfaces the order service error (e.g. not found / not owner).
func TestFetch_Order_ServiceError(t *testing.T) {
	repo := &fakeOrderRepo{getErr: errors.New("order not found")}
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(repo)})
	resp := callTool(t, newEmployeeCtx(), srv, "fetch", map[string]any{"id": "order:o-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "order not found")
}

// fetchVendor rejects a role that cannot read vendors.
func TestFetch_Vendor_RoleGate(t *testing.T) {
	svc := &vendor.Service{Vendors: &fakeVendorRepo{}, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, vendorOpCtx(), srv, "fetch", map[string]any{"id": "vendor:v-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "role cannot read vendors")
}

// === audit.query service error ===

func TestAuditQuery_ServiceError(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{err: errors.New("db down")}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "audit.query", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "db down")
}

// === compliance: document.review missing document_id, anomaly.close service error ===

func TestDocumentReview_MissingDocumentID(t *testing.T) {
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	// status present but document_id omitted → RequireString("document_id") error.
	resp := callTool(t, adminCtx(), srv, "document.review", map[string]any{"status": "approved"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
}

func TestAnomalyClose_ServiceError(t *testing.T) {
	// pool Begin fails → CloseAnomaly's pgx.BeginFunc returns the error,
	// which the handler surfaces as a tool error.
	svc := complianceService(&fakeDocRepo{}, &fakeAnomalyRepo{}, &fakeAuditQuery{}, &fakeAuditTx{}, fakeBeginner{beginErr: errors.New("pool down")})
	srv := mcpserver.New(mcpserver.Deps{Compliance: svc})
	resp := callTool(t, adminCtx(), srv, "anomaly.close", map[string]any{"anomaly_id": "a-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "pool down")
}

// === feedback.file_complaint missing category / description ===

func TestFeedbackFileComplaint_MissingOrderID(t *testing.T) {
	svc := feedbackService(&fakeFeedbackOrders{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	// order_id omitted → RequireString("order_id") error.
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{"category": "quality", "description": "xxxxxx"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestFeedbackFileComplaint_MissingCategory(t *testing.T) {
	svc := feedbackService(&fakeFeedbackOrders{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	// order_id present, category omitted → RequireString("category") error.
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{"order_id": "o-1"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestFeedbackFileComplaint_MissingDescription(t *testing.T) {
	svc := feedbackService(&fakeFeedbackOrders{}, &fakeRatingRepo{}, &fakeComplaintRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Feedback: svc})
	// order_id + category present, description omitted → RequireString("description") error.
	resp := callTool(t, newEmployeeCtx(), srv, "feedback.file_complaint", map[string]any{"order_id": "o-1", "category": "quality"})
	assert.True(t, toolResult(t, resp).IsError)
}

// === menu: resolvePlant explicit arg, search bad date, get_item bad date,
// get_item service error, vendor.list_open no plant, ListPlants error ===

// menu.list_for_day with an explicit plant arg exercises resolvePlant's
// arg != "" branch (and overrides the caller's home plant).
func TestMenuListForDay_ExplicitPlantArg(t *testing.T) {
	repo := &fakeItemRepo{rows: []*menu.ActiveItemRow{seedMenuRow("m-1", "Sushi", 12000)}}
	srv := mcpserver.New(mcpserver.Deps{Menu: &menu.Service{Items: repo, Images: fakeImageRepo{}}})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.list_for_day", map[string]any{"plant": "OTHER-PLANT"})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, "OTHER-PLANT", out["plant"])
	assert.Equal(t, "OTHER-PLANT", repo.lastFilter.Plant)
}

func TestMenuSearch_BadDate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.search", map[string]any{"query": "x", "supply_date": "nope"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid date")
}

func TestMenuGetItem_BadDate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, nil)})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.get_item", map[string]any{"item_id": "m-1", "supply_date": "nope"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "invalid date")
}

func TestMenuGetItem_ServiceError(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Menu: menuServiceWith(nil, errors.New("db down"))})
	resp := callTool(t, newEmployeeCtx(), srv, "menu.get_item", map[string]any{"item_id": "m-1"})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "get item")
}

// vendor.list_open with no plant arg and no home plant → plant required.
func TestVendorListOpen_NoPlant(t *testing.T) {
	svc := &vendor.Service{Vendors: &fakeVendorRepo{}, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	u := &identity.User{ID: "u", Role: identity.RoleEmployee, Status: identity.StatusActive} // no plant
	ctx := idhttp.ContextWithUser(context.Background(), u)
	resp := callTool(t, ctx, srv, "vendor.list_open", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "plant required")
}

// buildVendorListForPlant skips a vendor whose ListPlants lookup errors.
func TestVendorListOpen_ListPlantsError(t *testing.T) {
	vendors := &fakeVendorRepo{vendors: []*vendor.Vendor{{ID: "v-1", DisplayName: "X", Status: vendor.StatusApproved}}}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{err: errors.New("plants down")}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, newEmployeeCtx(), srv, "vendor.list_open", map[string]any{})
	text := toolText(t, resp)
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &out))
	assert.Equal(t, float64(0), out["count"]) // vendor skipped on ListPlants error
}

// === payroll branches ===

func TestPayrollListBatches_ServiceError(t *testing.T) {
	batches := &fakeBatchRepo{listErr: errors.New("db down")}
	svc := payrollService(batches, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.list_batches", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "db down")
}

func TestPayrollResolveDispute_MissingDisputeID(t *testing.T) {
	svc := payrollService(&fakeBatchRepo{}, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	// dispute_id omitted → RequireString("dispute_id") error.
	resp := callTool(t, adminCtx(), srv, "payroll.resolve_dispute", map[string]any{"status": "resolved_reject"})
	assert.True(t, toolResult(t, resp).IsError)
}

func TestPayrollResolveDispute_ServiceError(t *testing.T) {
	// dispute not found in repo → ResolveDispute returns an error.
	svc := payrollService(&fakeBatchRepo{}, &fakeDisputeRepo{}, &fakeAuditTx{}, fakeBeginner{})
	srv := mcpserver.New(mcpserver.Deps{Payroll: svc})
	resp := callTool(t, adminCtx(), srv, "payroll.resolve_dispute", map[string]any{
		"dispute_id": "missing", "status": "resolved_reject",
	})
	assert.True(t, toolResult(t, resp).IsError)
}

// === settlement: missing period_start ===

func TestSettlementClosePeriod_MissingStart(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Settlement: settlementService(nil, nil, &fakeAuditTx{}, fakeBeginner{})})
	// period_start omitted → parsePeriodInput's RequireString error.
	resp := callTool(t, adminCtx(), srv, "settlement.close_period", map[string]any{"period_end": "2026-05-31"})
	assert.True(t, toolResult(t, resp).IsError)
}

// === vendor: list service error, reinstate missing vendor_id ===

func TestVendorList_ServiceError(t *testing.T) {
	vendors := &fakeVendorRepo{listErr: errors.New("db down")}
	svc := &vendor.Service{Vendors: vendors, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, adminCtx(), srv, "vendor.list", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "db down")
}

func TestVendorReinstate_MissingArg(t *testing.T) {
	svc := &vendor.Service{Vendors: &fakeVendorRepo{}, Plants: &fakePlantRepo{}, Operators: fakeOperatorRepo{}}
	srv := mcpserver.New(mcpserver.Deps{Vendor: svc})
	resp := callTool(t, adminCtx(), srv, "vendor.reinstate", map[string]any{})
	assert.True(t, toolResult(t, resp).IsError)
}

// === order.list_mine service-not-configured (employee ctx, nil order svc) ===

func TestOrderListMine_NotConfigured(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{})
	resp := callTool(t, newEmployeeCtx(), srv, "order.list_mine", map[string]any{})
	res := toolResult(t, resp)
	assert.True(t, res.IsError)
	assert.Contains(t, toolText(t, resp), "order service not configured")
}

// === order.place missing supply_date (plant present) ===

func TestOrderPlace_MissingSupplyDate(t *testing.T) {
	srv := mcpserver.New(mcpserver.Deps{Order: orderServiceWith(&fakeOrderRepo{})})
	resp := callTool(t, newEmployeeCtx(), srv, "order.place", map[string]any{"plant": "F12B-3F"})
	assert.True(t, toolResult(t, resp).IsError)
}
