package main

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGaussianCounts_DefaultScenario(t *testing.T) {
	counts := gaussianCounts(50000, 100, 25)
	if got := sumInts(counts); got != 50000 {
		t.Fatalf("sum = %d, want 50000", got)
	}
	if counts[0] != 118 || counts[49] != 836 || counts[50] != 836 || counts[99] != 118 {
		t.Fatalf("unexpected default distribution edges/center: p1=%d p50=%d p51=%d p100=%d",
			counts[0], counts[49], counts[50], counts[99])
	}
}

func TestMerchantPickupMatrix_PreservesPickupTotals(t *testing.T) {
	counts := gaussianCounts(50000, 100, 25)
	matrix := merchantPickupMatrix(counts, 100, 3)
	if len(matrix) != 100 {
		t.Fatalf("rows = %d, want 100", len(matrix))
	}
	total := 0
	for p, row := range matrix {
		if got := sumInts(row); got != counts[p] {
			t.Fatalf("pickup row %d sum = %d, want %d", p+1, got, counts[p])
		}
		total += sumInts(row)
	}
	if total != 50000 {
		t.Fatalf("matrix total = %d, want 50000", total)
	}
}

func TestBuildSetupReport_DefaultStage1Batches(t *testing.T) {
	cfg := config{
		RunID:           "test",
		Employees:       50000,
		Merchants:       100,
		PickupPoints:    100,
		ItemsPerVendor:  10,
		PickupSigma:     25,
		MerchantSigma:   3,
		Stage1BatchSize: 100,
	}
	counts := gaussianCounts(cfg.Employees, cfg.PickupPoints, cfg.PickupSigma)
	matrix := merchantPickupMatrix(counts, cfg.Merchants, cfg.MerchantSigma)
	plants := makePlants(cfg.RunID, counts)
	vendors := makeVendors(cfg.RunID, cfg.Merchants, cfg.ItemsPerVendor, matrix)
	employees := makeEmployees(cfg.RunID, plants)
	assignOrders(cfg, plants, vendors, employees, matrix)
	orders := make([]orderRecord, 0, len(employees))
	for _, e := range employees {
		orders = append(orders, orderRecord{ID: e.Order, UserID: e.ID, VendorID: e.Vendor, Plant: e.Plant, Status: "cutoff"})
	}
	rep := buildSetupReport(cfg, &scenarioData{Plants: plants, Vendors: vendors, Employees: employees, Orders: orders, Matrix: matrix})
	if rep.Orders != 50000 {
		t.Fatalf("orders = %d, want 50000", rep.Orders)
	}
	if rep.Stage1Batches != 552 {
		t.Fatalf("stage1 batches = %d, want 552", rep.Stage1Batches)
	}
	if rep.PickupDistribution.Max != 836 {
		t.Fatalf("pickup max = %d, want 836", rep.PickupDistribution.Max)
	}
}

func TestFrontendPickupSteps_MatchBrowserRequestModel(t *testing.T) {
	if len(frontendPickupSteps) != 10 {
		t.Fatalf("frontend pickup steps = %d, want 10", len(frontendPickupSteps))
	}

	task := pickupTask{OrderID: "ord-1"}
	got := make([]string, 0, len(frontendPickupSteps))
	orderCountingSteps := 0
	for _, step := range frontendPickupSteps {
		got = append(got, step.Operation())
		if step.CountsOrder {
			orderCountingSteps++
			if step.Path(task) != "/api/employee/orders/ord-1/pickup" {
				t.Fatalf("pickup path = %q", step.Path(task))
			}
		}
	}
	want := []string{
		"GET /me",
		"GET /api/employee/orders",
		"GET /api/plants",
		"GET /api/employee/home",
		"GET /me",
		"GET /me",
		"POST /api/employee/orders/{id}/pickup",
		"GET /me",
		"GET /api/employee/orders",
		"GET /api/plants",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("steps:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if orderCountingSteps != 1 {
		t.Fatalf("order-counting steps = %d, want 1", orderCountingSteps)
	}
}

func TestSummarizeMetrics_FrontendRequestsDoNotInflateOrders(t *testing.T) {
	results := []taskResult{
		{Operation: "GET /me", Status: 200, Latency: 1},
		{Operation: "GET /api/employee/orders", Status: 200, Latency: 1},
		{Operation: "POST /api/employee/orders/{id}/pickup", Status: 204, Latency: 1, TaskSize: 1},
	}
	m := summarizeMetrics("stage_2_take_food", results, 3)
	if m.Requests != 3 {
		t.Fatalf("requests = %d, want 3", m.Requests)
	}
	if m.Orders != 1 {
		t.Fatalf("orders = %d, want 1", m.Orders)
	}
	if m.ByOperation["GET /me"] != 1 || m.ByOperation["POST /api/employee/orders/{id}/pickup"] != 1 {
		t.Fatalf("unexpected operation counts: %#v", m.ByOperation)
	}
}

func TestNewHTTPClient_SizesConnectionPoolForConcurrency(t *testing.T) {
	client := newHTTPClient(30*time.Second, 1000)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if client.Timeout != 30*time.Second {
		t.Fatalf("timeout = %s, want 30s", client.Timeout)
	}
	if transport.MaxConnsPerHost != 1000 {
		t.Fatalf("max conns per host = %d, want 1000", transport.MaxConnsPerHost)
	}
	if transport.MaxIdleConnsPerHost != 1000 {
		t.Fatalf("max idle conns per host = %d, want 1000", transport.MaxIdleConnsPerHost)
	}
}

func TestGaussianCounts_LargerScenario(t *testing.T) {
	counts := gaussianCounts(10000, 200, 50)
	if got := sumInts(counts); got != 10000 {
		t.Fatalf("sum = %d, want 10000", got)
	}
	matrix := merchantPickupMatrix(counts, 200, 3)
	total := 0
	for p, row := range matrix {
		if got := sumInts(row); got != counts[p] {
			t.Fatalf("pickup row %d sum = %d, want %d", p+1, got, counts[p])
		}
		total += sumInts(row)
	}
	if total != 10000 {
		t.Fatalf("matrix total = %d, want 10000", total)
	}
}

func sumInts(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}
