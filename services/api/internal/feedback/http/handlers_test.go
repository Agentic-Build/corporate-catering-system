package feedbackhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	audit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	feedbackhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
)

// NOTE ON COVERAGE
//
// feedback.Service pairs every *write* (RateOrder/FileComplaint and all
// complaint transitions) with an audit row inside pgx.BeginFunc(ctx, s.Pool,
// ...). Pool is a txBeginner interface, so buildHandler injects a fakeBeginner
// that hands the write closure a no-op pgx.Tx (the repo fakes ignore the tx).
// This exercises both the pre-BeginFunc branches (auth guards, huma request
// validation, domain-error mappings) AND the 2xx success path of every write
// handler without a real database. The read-only handlers (getMyRating,
// listMyComplaints, listVendorComplaints, listEscalatedComplaints) never touch
// the Pool and are likewise covered end-to-end.

const (
	orderID       = "11111111-1111-1111-1111-111111111111"
	complaintID   = "22222222-2222-2222-2222-222222222222"
	employeeID    = "emp-1"
	otherEmployee = "emp-2"
	vendorID      = "vend-1"
)

// === Fakes (feedbackhttp_test can't import the feedback_test package's helpers) ===

type fakeRatingRepo struct {
	byOrder map[string]*feedback.Rating
	getErr  error
}

func newFakeRatingRepo() *fakeRatingRepo {
	return &fakeRatingRepo{byOrder: map[string]*feedback.Rating{}}
}

func (r *fakeRatingRepo) CreateTx(context.Context, pgx.Tx, *feedback.Rating) error { return nil }
func (r *fakeRatingRepo) GetByOrder(_ context.Context, id string) (*feedback.Rating, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if rat, ok := r.byOrder[id]; ok {
		clone := *rat
		return &clone, nil
	}
	return nil, feedback.ErrRatingNotFound
}
func (r *fakeRatingRepo) AggregateByVendorSince(context.Context, time.Time) ([]feedback.VendorRatingStat, error) {
	return nil, nil
}

type fakeComplaintRepo struct {
	byID     map[string]*feedback.Complaint
	byUser   map[string][]*feedback.Complaint
	byVendor map[string][]*feedback.Complaint
	byStatus map[feedback.ComplaintStatus][]*feedback.Complaint
	getErr   error
	listErr  error
}

func newFakeComplaintRepo() *fakeComplaintRepo {
	return &fakeComplaintRepo{
		byID:     map[string]*feedback.Complaint{},
		byUser:   map[string][]*feedback.Complaint{},
		byVendor: map[string][]*feedback.Complaint{},
		byStatus: map[feedback.ComplaintStatus][]*feedback.Complaint{},
	}
}

func (r *fakeComplaintRepo) CreateTx(context.Context, pgx.Tx, *feedback.Complaint) error { return nil }
func (r *fakeComplaintRepo) GetByID(_ context.Context, id string) (*feedback.Complaint, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	if c, ok := r.byID[id]; ok {
		clone := *c
		return &clone, nil
	}
	return nil, feedback.ErrComplaintNotFound
}
func (r *fakeComplaintRepo) UpdateStatusTx(context.Context, pgx.Tx, string, feedback.ComplaintStatus, feedback.ComplaintStatus, feedback.ComplaintUpdate) error {
	return nil
}
func (r *fakeComplaintRepo) ListByUser(_ context.Context, userID string) ([]*feedback.Complaint, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byUser[userID], nil
}
func (r *fakeComplaintRepo) ListByVendor(_ context.Context, vID string, _ []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.byVendor[vID], nil
}
func (r *fakeComplaintRepo) ListByStatus(_ context.Context, statuses []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*feedback.Complaint
	for _, s := range statuses {
		out = append(out, r.byStatus[s]...)
	}
	return out, nil
}
func (r *fakeComplaintRepo) CountByVendorSince(context.Context, time.Time) ([]feedback.VendorComplaintStat, error) {
	return nil, nil
}

type fakeOrderReader struct {
	byID map[string]*feedback.OrderInfo
	err  error
}

func newFakeOrderReader() *fakeOrderReader {
	return &fakeOrderReader{byID: map[string]*feedback.OrderInfo{}}
}

func (r *fakeOrderReader) GetOrderInfo(_ context.Context, id string) (*feedback.OrderInfo, error) {
	if r.err != nil {
		return nil, r.err
	}
	if o, ok := r.byID[id]; ok {
		clone := *o
		return &clone, nil
	}
	return nil, feedback.ErrOrderNotFound
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the repo fakes ignore the tx, so write happy paths run without a real
// DB.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeReverser records the orders whose payroll deduction was reversed so the
// compensating AdminResolveComplaint path can be exercised + asserted.
type fakeReverser struct{ reversed []string }

func (r *fakeReverser) ReverseOrder(_ context.Context, orderID string) error {
	r.reversed = append(r.reversed, orderID)
	return nil
}

// fakeAudit is the no-op audit sink every write path writes through.
type fakeAudit struct{}

func (fakeAudit) WriteTx(context.Context, pgx.Tx, audit.Entry) error {
	return nil
}

// === Harness ===

type fakes struct {
	ratings    *fakeRatingRepo
	complaints *fakeComplaintRepo
	orders     *fakeOrderReader
	svc        *feedback.Service
}

func employeeUser() *identity.User {
	return &identity.User{ID: employeeID, Role: identity.RoleEmployee}
}

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "vop-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

func adminUser() *identity.User {
	return &identity.User{ID: "adm-1", Role: identity.RoleWelfareAdmin}
}

// now is pinned so the 24h escalation gate is deterministic. Complaints are
// seeded with CreatedAt = now (so escalation is "too early") unless a test
// backdates them.
var now = time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)

// buildHandler wires the feedback API onto a chi router. When user != nil a
// middleware injects it into the request context exactly like AuthMiddleware.
// Pool is a fakeBeginner so write happy-paths run end-to-end (the repo fakes
// ignore the tx it hands them).
func buildHandler(t *testing.T, user *identity.User) (*httptest.Server, *fakes) {
	t.Helper()
	f := &fakes{
		ratings:    newFakeRatingRepo(),
		complaints: newFakeComplaintRepo(),
		orders:     newFakeOrderReader(),
	}
	f.svc = &feedback.Service{
		Pool:       fakeBeginner{},
		Ratings:    f.ratings,
		Complaints: f.complaints,
		Orders:     f.orders,
		Audit:      fakeAudit{},
		Clock:      fixedClock{t: now},
	}
	api := &feedbackhttp.API{Svc: f.svc}

	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, f
}

func (f *fakes) seedPickedUpOrder(id, owner string) {
	f.orders.byID[id] = &feedback.OrderInfo{ID: id, UserID: owner, VendorID: vendorID, Status: "picked_up"}
}

func (f *fakes) seedComplaint(c *feedback.Complaint) {
	clone := *c
	f.complaints.byID[c.ID] = &clone
}

func do(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// === rateOrder ===

func TestRateOrder_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestRateOrder_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRateOrder_BadUUID_422(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/not-a-uuid/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestRateOrder_ScoreOutOfRange_422(t *testing.T) {
	// huma enforces minimum/maximum before the handler runs.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":6}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestRateOrder_OrderNotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser()) // order not seeded
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRateOrder_NotOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedPickedUpOrder(orderID, otherEmployee) // owned by someone else
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRateOrder_NotPickedUp_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = &feedback.OrderInfo{ID: orderID, UserID: employeeID, VendorID: vendorID, Status: "placed"}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRateOrder_AlreadyRated_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedPickedUpOrder(orderID, employeeID)
	f.ratings.byOrder[orderID] = &feedback.Rating{ID: "r-1", OrderID: orderID, UserID: employeeID}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRateOrder_OrderReaderError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.err = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating", `{"score":4,"comment":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestRateOrder_OK_201(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedPickedUpOrder(orderID, employeeID) // owned, picked up, not yet rated
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/rating",
		`{"score":4,"comment":"tasty"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Rating struct {
			OrderID  string `json:"order_id"`
			UserID   string `json:"user_id"`
			VendorID string `json:"vendor_id"`
			Score    int    `json:"score"`
			Comment  string `json:"comment"`
		} `json:"rating"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, orderID, out.Rating.OrderID)
	assert.Equal(t, employeeID, out.Rating.UserID)
	assert.Equal(t, vendorID, out.Rating.VendorID)
	assert.Equal(t, 4, out.Rating.Score)
	assert.Equal(t, "tasty", out.Rating.Comment)
}

// === getMyRating (read-only: happy path covered) ===

func TestGetMyRating_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetMyRating_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGetMyRating_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.ratings.byOrder[orderID] = &feedback.Rating{
		ID: "r-1", OrderID: orderID, UserID: employeeID, VendorID: vendorID,
		Score: 5, Comment: "great", CreatedAt: now,
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Rating struct {
			ID    string `json:"id"`
			Score int    `json:"score"`
		} `json:"rating"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "r-1", out.Rating.ID)
	assert.Equal(t, 5, out.Rating.Score)
}

func TestGetMyRating_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser()) // no rating seeded
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetMyRating_ForeignUser_404(t *testing.T) {
	// Don't leak another employee's rating: handler maps to 404.
	srv, f := buildHandler(t, employeeUser())
	f.ratings.byOrder[orderID] = &feedback.Rating{ID: "r-1", OrderID: orderID, UserID: otherEmployee}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGetMyRating_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.ratings.getErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/orders/"+orderID+"/rating", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === fileComplaint ===

func TestFileComplaint_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestFileComplaint_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestFileComplaint_BadCategory_422(t *testing.T) {
	// huma enforces the category enum before the handler.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"nonsense","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestFileComplaint_ShortDescription_422(t *testing.T) {
	// huma enforces minLength before the handler.
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestFileComplaint_OrderNotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestFileComplaint_NotOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedPickedUpOrder(orderID, otherEmployee)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestFileComplaint_NotPickedUp_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.byID[orderID] = &feedback.OrderInfo{ID: orderID, UserID: employeeID, VendorID: vendorID, Status: "placed"}
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestFileComplaint_OrderReaderError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.orders.err = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"valid description"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestFileComplaint_OK_201(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedPickedUpOrder(orderID, employeeID)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/orders/"+orderID+"/complaint",
		`{"category":"quality","description":"the meal was cold"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Complaint struct {
			OrderID     string `json:"order_id"`
			UserID      string `json:"user_id"`
			VendorID    string `json:"vendor_id"`
			Category    string `json:"category"`
			Description string `json:"description"`
			Status      string `json:"status"`
		} `json:"complaint"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, orderID, out.Complaint.OrderID)
	assert.Equal(t, employeeID, out.Complaint.UserID)
	assert.Equal(t, vendorID, out.Complaint.VendorID)
	assert.Equal(t, "quality", out.Complaint.Category)
	assert.Equal(t, "the meal was cold", out.Complaint.Description)
	assert.Equal(t, "open", out.Complaint.Status)
}

// === listMyComplaints (read-only: happy path covered) ===

func TestListMyComplaints_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListMyComplaints_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListMyComplaints_OK(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.complaints.byUser[employeeID] = []*feedback.Complaint{
		{ID: complaintID, OrderID: orderID, UserID: employeeID, VendorID: vendorID,
			Category: feedback.CategoryQuality, Description: "bad", Status: feedback.StatusOpen, CreatedAt: now},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/complaints", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, complaintID, out.Items[0].ID)
	assert.Equal(t, "open", out.Items[0].Status)
}

func TestListMyComplaints_EmptyIsArray(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/complaints", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Items)
	assert.NotNil(t, out.Items)
}

func TestListMyComplaints_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.complaints.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === escalateComplaint ===

func TestEscalateComplaint_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestEscalateComplaint_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEscalateComplaint_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestEscalateComplaint_NotOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: otherEmployee, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestEscalateComplaint_InvalidTransition_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: employeeID, Status: feedback.StatusResolved, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestEscalateComplaint_TooEarly_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	// CreatedAt = now, gate is 24h -> too early.
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: employeeID, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestEscalateComplaint_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.complaints.getErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestEscalateComplaint_OK_204(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	// CreatedAt 2 days ago clears the 24h gate; owned + open -> escalated.
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: employeeID, Status: feedback.StatusOpen, CreatedAt: now.Add(-48 * time.Hour)})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/escalate", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// === resolveMyComplaint ===

func TestResolveMyComplaint_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestResolveMyComplaint_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestResolveMyComplaint_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestResolveMyComplaint_NotOwner_403(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: otherEmployee, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestResolveMyComplaint_InvalidTransition_409(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: employeeID, Status: feedback.StatusEscalated, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestResolveMyComplaint_OK_204(t *testing.T) {
	srv, f := buildHandler(t, employeeUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, UserID: employeeID, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/employee/complaints/"+complaintID+"/resolve", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// === listVendorComplaints (read-only: happy path covered) ===

func TestListVendorComplaints_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListVendorComplaints_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListVendorComplaints_NoVendorBinding_403(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "vop-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListVendorComplaints_OK(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.complaints.byVendor[vendorID] = []*feedback.Complaint{
		{ID: complaintID, OrderID: orderID, UserID: employeeID, VendorID: vendorID,
			Category: feedback.CategoryHygiene, Description: "dirty", Status: feedback.StatusOpen, CreatedAt: now},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID       string `json:"id"`
			VendorID string `json:"vendor_id"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, vendorID, out.Items[0].VendorID)
}

func TestListVendorComplaints_WithStatusFilter(t *testing.T) {
	// Exercises the status-query branch (in.Status != "").
	srv, f := buildHandler(t, vendorUser())
	f.complaints.byVendor[vendorID] = []*feedback.Complaint{
		{ID: complaintID, VendorID: vendorID, Status: feedback.StatusOpen, CreatedAt: now},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints?status=open", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []any `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Items, 1)
}

func TestListVendorComplaints_BadStatus_422(t *testing.T) {
	// huma enforces the status enum.
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints?status=bogus", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestListVendorComplaints_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.complaints.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === respondToComplaint ===

func TestRespondToComplaint_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestRespondToComplaint_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRespondToComplaint_NoVendorBinding_403(t *testing.T) {
	srv, _ := buildHandler(t, &identity.User{ID: "vop-1", Role: identity.RoleVendorOperator})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRespondToComplaint_ShortResponse_422(t *testing.T) {
	// huma enforces minLength:"5" before the handler.
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"hi"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestRespondToComplaint_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, vendorUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are very sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestRespondToComplaint_WrongVendor_403(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, VendorID: "other-vendor", Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are very sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRespondToComplaint_NotOpen_409(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, VendorID: vendorID, Status: feedback.StatusVendorResponded, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are very sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRespondToComplaint_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	f.complaints.getErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are very sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestRespondToComplaint_OK_204(t *testing.T) {
	srv, f := buildHandler(t, vendorUser())
	// own vendor + open -> vendor_responded.
	f.seedComplaint(&feedback.Complaint{ID: complaintID, VendorID: vendorID, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/complaints/"+complaintID+"/respond",
		`{"response":"we are very sorry"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// === listEscalatedComplaints (read-only: happy path covered) ===

func TestListEscalatedComplaints_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListEscalatedComplaints_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListEscalatedComplaints_OK(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	f.complaints.byStatus[feedback.StatusEscalated] = []*feedback.Complaint{
		{ID: complaintID, OrderID: orderID, UserID: employeeID, VendorID: vendorID,
			Status: feedback.StatusEscalated, Category: feedback.CategoryOther, Description: "x", CreatedAt: now},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/complaints", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, "escalated", out.Items[0].Status)
}

func TestListEscalatedComplaints_ResolvedBySerialized(t *testing.T) {
	// A resolved complaint carries a non-nil ResolvedBy; toComplaintDTO must
	// copy it into the resolved_by field.
	srv, f := buildHandler(t, adminUser())
	resolver := "adm-7"
	resolvedAt := now.Add(time.Hour)
	f.complaints.byStatus[feedback.StatusEscalated] = []*feedback.Complaint{
		{ID: complaintID, OrderID: orderID, UserID: employeeID, VendorID: vendorID,
			Status: feedback.StatusResolved, Category: feedback.CategoryOther, Description: "x",
			Resolution: "committee decision", ResolvedBy: &resolver, ResolvedAt: &resolvedAt, CreatedAt: now},
	}
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/complaints", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID         string  `json:"id"`
			ResolvedBy *string `json:"resolved_by"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	require.NotNil(t, out.Items[0].ResolvedBy)
	assert.Equal(t, resolver, *out.Items[0].ResolvedBy)
}

func TestListEscalatedComplaints_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	f.complaints.listErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/complaints", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// === adminResolveComplaint ===

func TestAdminResolveComplaint_Unauthenticated(t *testing.T) {
	srv, _ := buildHandler(t, nil)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAdminResolveComplaint_WrongRole(t *testing.T) {
	srv, _ := buildHandler(t, employeeUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAdminResolveComplaint_ShortResolution_422(t *testing.T) {
	// huma enforces minLength:"5" before the handler.
	srv, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"no"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
}

func TestAdminResolveComplaint_NotFound_404(t *testing.T) {
	srv, _ := buildHandler(t, adminUser())
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAdminResolveComplaint_NotEscalated_409(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, VendorID: vendorID, Status: feedback.StatusOpen, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestAdminResolveComplaint_CompensateNilReverser_500(t *testing.T) {
	// compensate=true with a nil Reverser is a config error -> generic 500.
	srv, f := buildHandler(t, adminUser())
	f.seedComplaint(&feedback.Complaint{ID: complaintID, OrderID: orderID, VendorID: vendorID, Status: feedback.StatusEscalated, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision","compensate":true}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestAdminResolveComplaint_RepoError_500(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	f.complaints.getErr = errors.New("db down")
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestAdminResolveComplaint_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	// escalated -> resolved, no compensation (Reverser untouched).
	f.seedComplaint(&feedback.Complaint{ID: complaintID, OrderID: orderID, VendorID: vendorID, Status: feedback.StatusEscalated, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestAdminResolveComplaint_Compensate_OK_204(t *testing.T) {
	srv, f := buildHandler(t, adminUser())
	rev := &fakeReverser{}
	f.svc.Reverser = rev
	f.seedComplaint(&feedback.Complaint{ID: complaintID, OrderID: orderID, VendorID: vendorID, Status: feedback.StatusEscalated, CreatedAt: now})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/complaints/"+complaintID+"/resolve",
		`{"resolution":"committee decision","compensate":true}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, []string{orderID}, rev.reversed) // payroll reversal fired once
}
