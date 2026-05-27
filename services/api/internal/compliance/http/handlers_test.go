package chttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	chttp "github.com/takalawang/corporate-catering-system/services/api/internal/compliance/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// Valid UUIDs — huma validates path params tagged format:"uuid" before the
// handler runs, so URLs must carry real UUID strings.
const (
	vendorID = "11111111-1111-1111-1111-111111111111"
	docID    = "22222222-2222-2222-2222-222222222222"
	anomID   = "33333333-3333-3333-3333-333333333333"
)

// ----- Fakes (chttp_test cannot import the compliance_test package's fakes) -----

// fakeDocs implements compliance.DocumentRepository. Only the methods the
// handler paths reach (ListByVendor, GetByID) carry behaviour; the rest exist
// to satisfy the interface and are never hit by the DB-free handler tests.
type fakeDocs struct {
	listByVendor func(ctx context.Context, vendorID string, includeAll bool) ([]*compliance.Document, error)
	getByID      func(ctx context.Context, id string) (*compliance.Document, error)
	createTx     func(ctx context.Context, d *compliance.Document) error
}

func (f *fakeDocs) Create(context.Context, *compliance.Document) error { return nil }
func (f *fakeDocs) CreateTx(ctx context.Context, _ pgx.Tx, d *compliance.Document) error {
	if f.createTx != nil {
		return f.createTx(ctx, d)
	}
	return nil
}
func (f *fakeDocs) GetByID(ctx context.Context, id string) (*compliance.Document, error) {
	if f.getByID != nil {
		return f.getByID(ctx, id)
	}
	return nil, compliance.ErrDocumentNotFound
}
func (f *fakeDocs) ListByVendor(ctx context.Context, vendorID string, includeAll bool) ([]*compliance.Document, error) {
	if f.listByVendor != nil {
		return f.listByVendor(ctx, vendorID, includeAll)
	}
	return nil, nil
}
func (f *fakeDocs) UpdateStatus(context.Context, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (f *fakeDocs) UpdateStatusTx(context.Context, pgx.Tx, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (f *fakeDocs) ListExpiringBefore(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}
func (f *fakeDocs) ListPastExpiry(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}

// fakeAnomaly implements compliance.AnomalyRepository. List and GetByID carry
// behaviour; the *Tx methods are never reached (they live behind pgx.BeginFunc
// on a nil Pool, a path the handler tests deliberately avoid).
type fakeAnomaly struct {
	list    func(ctx context.Context, st []compliance.AnomalyStatus, sev []compliance.AnomalySeverity) ([]*compliance.Anomaly, error)
	getByID func(ctx context.Context, id string) (*compliance.Anomaly, error)
}

func (f *fakeAnomaly) Open(context.Context, *compliance.Anomaly) error { return nil }
func (f *fakeAnomaly) GetByID(ctx context.Context, id string) (*compliance.Anomaly, error) {
	if f.getByID != nil {
		return f.getByID(ctx, id)
	}
	return nil, compliance.ErrAnomalyNotFound
}
func (f *fakeAnomaly) List(ctx context.Context, st []compliance.AnomalyStatus, sev []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	if f.list != nil {
		return f.list(ctx, st, sev)
	}
	return nil, nil
}
func (f *fakeAnomaly) Triage(context.Context, string, string, string) error           { return nil }
func (f *fakeAnomaly) TriageTx(context.Context, pgx.Tx, string, string, string) error { return nil }
func (f *fakeAnomaly) Close(context.Context, string, string, string) error            { return nil }
func (f *fakeAnomaly) CloseTx(context.Context, pgx.Tx, string, string, string) error  { return nil }

// fakeVendors implements compliance.VendorReader.
type fakeVendors struct {
	getByID func(ctx context.Context, id string) (*vendor.Vendor, error)
}

func (f *fakeVendors) GetByID(ctx context.Context, id string) (*vendor.Vendor, error) {
	if f.getByID != nil {
		return f.getByID(ctx, id)
	}
	return nil, vendor.ErrVendorNotFound
}

// fakeAuditQry implements compliance.AuditQuery.
type fakeAuditQry struct {
	list func(ctx context.Context, filter compliance.AuditFilter) ([]compliance.AuditRow, error)
}

func (f *fakeAuditQry) List(ctx context.Context, filter compliance.AuditFilter) ([]compliance.AuditRow, error) {
	if f.list != nil {
		return f.list(ctx, filter)
	}
	return nil, nil
}

// fixedClock pins Now() so MerchantCompliance's computeWarnings is deterministic.
type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx so the upload/review/triage/close write paths run without a real DB;
// the repo fakes ignore the tx.
type fakeBeginner struct{}

func (fakeBeginner) Begin(context.Context) (pgx.Tx, error) { return fakeTx{}, nil }

type fakeTx struct{ pgx.Tx }

func (fakeTx) Commit(context.Context) error   { return nil }
func (fakeTx) Rollback(context.Context) error { return nil }

// fakeStore stands in for *storage.S3Client; it returns a synthetic URI without
// reaching S3 so the upload write paths complete DB- and network-free.
type fakeStore struct{}

func (fakeStore) PutObject(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	return "s3://bucket/" + key, nil
}

// fakeAudit / fakeOutbox satisfy the tx-scoped write interfaces; they ignore the
// no-op tx and record nothing.
type fakeAudit struct{}

func (fakeAudit) WriteTx(context.Context, pgx.Tx, *string, *string, string, string, string, map[string]any, string) error {
	return nil
}

type fakeOutbox struct{}

func (fakeOutbox) AppendTx(context.Context, pgx.Tx, string, string, string, map[string]any, map[string]any) error {
	return nil
}

// fakeSuspender satisfies compliance.VendorSuspender for the triage "suspend"
// governance path. err lets a test exercise the propagated-error branch.
type fakeSuspender struct {
	called bool
	err    error
}

func (f *fakeSuspender) Suspend(context.Context, string, string) error {
	f.called = true
	return f.err
}

// ----- Harness -----

func adminUser() *identity.User {
	return &identity.User{ID: "admin-1", Role: identity.RoleWelfareAdmin}
}

func vendorUser() *identity.User {
	v := vendorID
	return &identity.User{ID: "op-1", Role: identity.RoleVendorOperator, VendorID: &v}
}

// buildHandler wires the compliance API onto a chi router with the given fakes.
// When user != nil a middleware injects it like AuthMiddleware does. Storage,
// Pool, Audit, Outbox and VendorGov are intentionally left nil: the covered
// handler branches never reach them.
func buildHandler(t *testing.T, user *identity.User, svc *compliance.Service) *httptest.Server {
	t.Helper()
	api := &chttp.API{Svc: svc}

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
	return srv
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

// genericErr is a non-sentinel error: it must map to 500.
var genericErr = errors.New("db down")

// =====================================================================
// listVendorDocuments — GET /api/admin/vendors/{vendor_id}/documents
// =====================================================================

func TestListDocuments_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/documents", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListDocuments_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/documents", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListDocuments_OK(t *testing.T) {
	docs := &fakeDocs{
		listByVendor: func(_ context.Context, vid string, _ bool) ([]*compliance.Document, error) {
			assert.Equal(t, vendorID, vid)
			return []*compliance.Document{
				{ID: docID, VendorID: vendorID, Kind: compliance.DocKindInsurance, Filename: "ins.pdf", Status: compliance.DocStatusPending},
			}, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: docs})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/documents", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, docID, out.Items[0].ID)
	assert.Equal(t, "insurance", out.Items[0].Kind)
	assert.Equal(t, "pending", out.Items[0].Status)
}

func TestListDocuments_IncludeAll_PassedThrough(t *testing.T) {
	var gotIncludeAll bool
	docs := &fakeDocs{
		listByVendor: func(_ context.Context, _ string, includeAll bool) ([]*compliance.Document, error) {
			gotIncludeAll = includeAll
			return nil, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: docs})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/documents?include_all=true", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, gotIncludeAll, "include_all query should reach the repo")
}

func TestListDocuments_RepoError_500(t *testing.T) {
	docs := &fakeDocs{
		listByVendor: func(_ context.Context, _ string, _ bool) ([]*compliance.Document, error) {
			return nil, genericErr
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: docs})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/vendors/"+vendorID+"/documents", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =====================================================================
// uploadVendorDocument — POST /api/admin/vendors/{vendor_id}/documents
// (admin). Pre-service branches only: auth, base64, expires_at, and the
// resupply GetByID path, which run before the nil Pool/Storage is touched.
// =====================================================================

func TestUploadDocument_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUploadDocument_WrongRole(t *testing.T) {
	srv := buildHandler(t, &identity.User{ID: "e-1", Role: identity.RoleEmployee}, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestUploadDocument_InvalidBase64_400(t *testing.T) {
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"!!!not-base64!!!"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestUploadDocument_InvalidExpiresAt_400(t *testing.T) {
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8=","expires_at":"31-12-2026"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

// =====================================================================
// reviewVendorDocument — POST /api/admin/documents/{id}/review.
// Happy path runs pgx.BeginFunc on a nil Pool, so only auth is covered.
// =====================================================================

func TestReviewDocument_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/documents/"+docID+"/review",
		`{"status":"approved","notes":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestReviewDocument_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/documents/"+docID+"/review",
		`{"status":"approved","notes":"ok"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// =====================================================================
// listAnomalies — GET /api/admin/anomalies
// =====================================================================

func TestListAnomalies_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/anomalies", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestListAnomalies_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/anomalies", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestListAnomalies_OK_NoFilters(t *testing.T) {
	now := time.Now().UTC()
	anom := &fakeAnomaly{
		list: func(_ context.Context, st []compliance.AnomalyStatus, sev []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
			assert.Empty(t, st, "no status filter when query empty")
			assert.Empty(t, sev, "no severity filter when query empty")
			return []*compliance.Anomaly{
				{ID: anomID, Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
					Severity: compliance.SeverityHigh, Status: compliance.AnomalyOpen, CreatedAt: now},
			}, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Anomaly: anom})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/anomalies", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID          string         `json:"id"`
			Severity    string         `json:"severity"`
			Status      string         `json:"status"`
			Payload     map[string]any `json:"payload"`
			EvidenceURI []string       `json:"evidence_uri"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, anomID, out.Items[0].ID)
	assert.Equal(t, "high", out.Items[0].Severity)
	assert.Equal(t, "open", out.Items[0].Status)
	assert.NotNil(t, out.Items[0].Payload, "nil payload must serialize as {}")
	assert.NotNil(t, out.Items[0].EvidenceURI, "nil evidence must serialize as []")
}

func TestListAnomalies_FiltersPassedThrough(t *testing.T) {
	var gotStatuses []compliance.AnomalyStatus
	var gotSeverities []compliance.AnomalySeverity
	anom := &fakeAnomaly{
		list: func(_ context.Context, st []compliance.AnomalyStatus, sev []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
			gotStatuses = st
			gotSeverities = sev
			return nil, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Anomaly: anom})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/anomalies?status=open&severity=critical", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []compliance.AnomalyStatus{compliance.AnomalyOpen}, gotStatuses)
	assert.Equal(t, []compliance.AnomalySeverity{compliance.SeverityCritical}, gotSeverities)
}

func TestListAnomalies_RepoError_500(t *testing.T) {
	anom := &fakeAnomaly{
		list: func(_ context.Context, _ []compliance.AnomalyStatus, _ []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
			return nil, genericErr
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Anomaly: anom})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/anomalies", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =====================================================================
// triageAnomaly — POST /api/admin/anomalies/{id}/triage.
// GetByID runs before the nil-Pool BeginFunc, so not-found / generic error
// branches are reachable; the success path is not (it hits the nil Pool).
// =====================================================================

func TestTriageAnomaly_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage", `{"notes":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestTriageAnomaly_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage", `{"notes":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestTriageAnomaly_NotFound_404(t *testing.T) {
	anom := &fakeAnomaly{
		getByID: func(_ context.Context, _ string) (*compliance.Anomaly, error) {
			return nil, compliance.ErrAnomalyNotFound
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Anomaly: anom})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage", `{"notes":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestTriageAnomaly_RepoError_500(t *testing.T) {
	anom := &fakeAnomaly{
		getByID: func(_ context.Context, _ string) (*compliance.Anomaly, error) {
			return nil, genericErr
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{Anomaly: anom})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage", `{"notes":"x"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =====================================================================
// closeAnomaly — POST /api/admin/anomalies/{id}/close.
// Happy path runs pgx.BeginFunc on a nil Pool, so only auth is covered.
// =====================================================================

func TestCloseAnomaly_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/close", `{"notes":"done"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCloseAnomaly_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Anomaly: &fakeAnomaly{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/close", `{"notes":"done"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

// =====================================================================
// listAuditEvents — GET /api/admin/audit
// =====================================================================

func TestAudit_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{AuditQry: &fakeAuditQry{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAudit_WrongRole(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{AuditQry: &fakeAuditQry{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAudit_OK(t *testing.T) {
	now := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	role := "welfare_admin"
	qry := &fakeAuditQry{
		list: func(_ context.Context, filter compliance.AuditFilter) ([]compliance.AuditRow, error) {
			assert.Equal(t, "vendor", filter.TargetKind)
			assert.Equal(t, vendorID, filter.TargetID)
			assert.Equal(t, 50, filter.Limit)
			return []compliance.AuditRow{
				{ID: 7, ActorRole: &role, Action: "vendor.warning", TargetKind: "vendor", TargetID: vendorID, At: now},
			}, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{AuditQry: qry})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit?target_kind=vendor&target_id="+vendorID+"&limit=50", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Items []struct {
			ID      int64          `json:"id"`
			Action  string         `json:"action"`
			Payload map[string]any `json:"payload"`
		} `json:"items"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Items, 1)
	assert.Equal(t, int64(7), out.Items[0].ID)
	assert.Equal(t, "vendor.warning", out.Items[0].Action)
	assert.NotNil(t, out.Items[0].Payload, "nil payload serializes as {}")
}

func TestAudit_WithSince_OK(t *testing.T) {
	var gotSince time.Time
	qry := &fakeAuditQry{
		list: func(_ context.Context, filter compliance.AuditFilter) ([]compliance.AuditRow, error) {
			gotSince = filter.Since
			return nil, nil
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{AuditQry: qry})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit?since=2026-05-01T00:00:00Z", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), gotSince.UTC())
}

func TestAudit_BadSince_400(t *testing.T) {
	srv := buildHandler(t, adminUser(), &compliance.Service{AuditQry: &fakeAuditQry{}})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit?since=not-a-time", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAudit_NotWired_500(t *testing.T) {
	// AuditQry left nil → service returns a generic error → 500.
	srv := buildHandler(t, adminUser(), &compliance.Service{})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestAudit_RepoError_500(t *testing.T) {
	qry := &fakeAuditQry{
		list: func(_ context.Context, _ compliance.AuditFilter) ([]compliance.AuditRow, error) {
			return nil, genericErr
		},
	}
	srv := buildHandler(t, adminUser(), &compliance.Service{AuditQry: qry})
	resp := do(t, http.MethodGet, srv.URL+"/api/admin/audit", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =====================================================================
// getMerchantCompliance — GET /api/merchant/compliance
// =====================================================================

func TestMerchantCompliance_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantCompliance_WrongRole(t *testing.T) {
	srv := buildHandler(t, adminUser(), &compliance.Service{})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantCompliance_NoVendorBinding_403(t *testing.T) {
	srv := buildHandler(t, &identity.User{ID: "op-1", Role: identity.RoleVendorOperator}, &compliance.Service{})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantCompliance_OK(t *testing.T) {
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	docs := &fakeDocs{
		listByVendor: func(_ context.Context, vid string, includeAll bool) ([]*compliance.Document, error) {
			assert.Equal(t, vendorID, vid)
			assert.False(t, includeAll, "merchant self-view requests live docs only")
			// Only insurance on file (rejected) — expect a rejected warning plus
			// missing warnings for the other three required kinds.
			return []*compliance.Document{
				{ID: docID, VendorID: vendorID, Kind: compliance.DocKindInsurance, Status: compliance.DocStatusRejected},
			}, nil
		},
	}
	vendors := &fakeVendors{
		getByID: func(_ context.Context, id string) (*vendor.Vendor, error) {
			assert.Equal(t, vendorID, id)
			return &vendor.Vendor{ID: vendorID, DisplayName: "Acme Catering", Status: vendor.StatusApproved}, nil
		},
	}
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: docs, Vendors: vendors, Clock: fixedClock{now}})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Vendor struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Status      string `json:"status"`
		} `json:"vendor"`
		Documents []struct {
			ID string `json:"id"`
		} `json:"documents"`
		Warnings []struct {
			Kind     string `json:"kind"`
			Severity string `json:"severity"`
		} `json:"warnings"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, vendorID, out.Vendor.ID)
	assert.Equal(t, "Acme Catering", out.Vendor.DisplayName)
	assert.Equal(t, "approved", out.Vendor.Status)
	require.Len(t, out.Documents, 1)
	assert.Equal(t, docID, out.Documents[0].ID)

	var rejected, missing int
	for _, w := range out.Warnings {
		switch w.Kind {
		case "document_rejected":
			rejected++
		case "document_missing":
			missing++
		}
	}
	assert.Equal(t, 1, rejected, "rejected insurance should warn")
	assert.Equal(t, 3, missing, "three other required kinds missing")
}

func TestMerchantCompliance_VendorNotFound_404(t *testing.T) {
	vendors := &fakeVendors{
		getByID: func(_ context.Context, _ string) (*vendor.Vendor, error) {
			return nil, vendor.ErrVendorNotFound
		},
	}
	srv := buildHandler(t, vendorUser(), &compliance.Service{Vendors: vendors, Clock: fixedClock{time.Now()}})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestMerchantCompliance_DocsError_500(t *testing.T) {
	vendors := &fakeVendors{
		getByID: func(_ context.Context, _ string) (*vendor.Vendor, error) {
			return &vendor.Vendor{ID: vendorID, DisplayName: "Acme", Status: vendor.StatusApproved}, nil
		},
	}
	docs := &fakeDocs{
		listByVendor: func(_ context.Context, _ string, _ bool) ([]*compliance.Document, error) {
			return nil, genericErr
		},
	}
	srv := buildHandler(t, vendorUser(), &compliance.Service{Vendors: vendors, Docs: docs, Clock: fixedClock{time.Now()}})
	resp := do(t, http.MethodGet, srv.URL+"/api/merchant/compliance", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// =====================================================================
// uploadMerchantDocument — POST /api/merchant/documents.
// Pre-service branches: auth, base64, expires_at, and the resupply GetByID
// path (which runs before the nil Pool/Storage is touched).
// =====================================================================

func TestMerchantUpload_Unauthenticated(t *testing.T) {
	srv := buildHandler(t, nil, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMerchantUpload_WrongRole(t *testing.T) {
	srv := buildHandler(t, adminUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantUpload_NoVendorBinding_403(t *testing.T) {
	srv := buildHandler(t, &identity.User{ID: "op-1", Role: identity.RoleVendorOperator}, &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestMerchantUpload_InvalidBase64_400(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"!!!"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMerchantUpload_InvalidExpiresAt_400(t *testing.T) {
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: &fakeDocs{}})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8=","expires_at":"2026/01/01"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMerchantUpload_ResupplyWrongVendor_409(t *testing.T) {
	// Supersedes a document owned by a different vendor → validateResupplyTarget
	// returns ErrInvalidResupply → 409. This runs in UploadDocument before the
	// nil Pool/Storage is ever touched.
	docs := &fakeDocs{
		getByID: func(_ context.Context, _ string) (*compliance.Document, error) {
			return &compliance.Document{ID: docID, VendorID: "other-vendor", Status: compliance.DocStatusApproved}, nil
		},
	}
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: docs})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8=","supersedes":"`+docID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestMerchantUpload_ResupplyTargetNotFound_404(t *testing.T) {
	docs := &fakeDocs{
		getByID: func(_ context.Context, _ string) (*compliance.Document, error) {
			return nil, compliance.ErrDocumentNotFound
		},
	}
	srv := buildHandler(t, vendorUser(), &compliance.Service{Docs: docs})
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"insurance","filename":"a.pdf","content_base64":"aGVsbG8=","supersedes":"`+docID+`"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// =====================================================================
// Write-path success (2xx). Pool is a fakeBeginner and Storage a fakeStore,
// so upload/review/triage/close run their full pgx.BeginFunc closures (and,
// for upload, the S3 PutObject) end-to-end without a DB or S3.
// =====================================================================

// uploadFakes returns a Service wired for the upload write path: a docs repo
// whose CreateTx stamps an ID (so the returned DTO carries it), plus the
// fake pool, store, audit and a pinned clock.
func uploadFakes() (*fakeDocs, *compliance.Service) {
	docs := &fakeDocs{}
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    docs,
		Storage: fakeStore{},
		Audit:   fakeAudit{},
		Clock:   fixedClock{time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)},
	}
	return docs, svc
}

func TestUploadDocument_OK_201(t *testing.T) {
	docs, svc := uploadFakes()
	docs.createTx = func(_ context.Context, d *compliance.Document) error {
		d.ID = docID // simulate DB assigning the row id
		return nil
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/vendors/"+vendorID+"/documents",
		`{"kind":"insurance","filename":"ins.pdf","content_base64":"aGVsbG8=","expires_at":"2026-12-31"}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Document struct {
			ID        string  `json:"id"`
			VendorID  string  `json:"vendor_id"`
			Kind      string  `json:"kind"`
			Filename  string  `json:"filename"`
			BlobURI   string  `json:"blob_uri"`
			Status    string  `json:"status"`
			ExpiresAt *string `json:"expires_at"`
		} `json:"document"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, docID, out.Document.ID)
	assert.Equal(t, vendorID, out.Document.VendorID)
	assert.Equal(t, "insurance", out.Document.Kind)
	assert.Equal(t, "ins.pdf", out.Document.Filename)
	assert.Equal(t, "pending", out.Document.Status)
	assert.True(t, strings.HasPrefix(out.Document.BlobURI, "s3://bucket/vendor-docs/"+vendorID+"/"),
		"blob_uri should be the synthetic store URI for the vendor key prefix")
	require.NotNil(t, out.Document.ExpiresAt)
	assert.Equal(t, "2026-12-31", *out.Document.ExpiresAt)
}

func TestMerchantUpload_OK_201(t *testing.T) {
	docs, svc := uploadFakes()
	docs.createTx = func(_ context.Context, d *compliance.Document) error {
		assert.Equal(t, vendorID, d.VendorID, "vendor resolved from session, not input")
		d.ID = docID
		return nil
	}
	srv := buildHandler(t, vendorUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/documents",
		`{"kind":"food_safety_permit","filename":"permit.pdf","content_base64":"aGVsbG8="}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out struct {
		Document struct {
			ID       string `json:"id"`
			VendorID string `json:"vendor_id"`
			Kind     string `json:"kind"`
			BlobURI  string `json:"blob_uri"`
			Status   string `json:"status"`
		} `json:"document"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, docID, out.Document.ID)
	assert.Equal(t, vendorID, out.Document.VendorID)
	assert.Equal(t, "food_safety_permit", out.Document.Kind)
	assert.Equal(t, "pending", out.Document.Status)
	assert.True(t, strings.HasPrefix(out.Document.BlobURI, "s3://bucket/vendor-docs/"+vendorID+"/"))
}

func TestReviewDocument_OK_204(t *testing.T) {
	svc := &compliance.Service{
		Pool:   fakeBeginner{},
		Docs:   &fakeDocs{},
		Audit:  fakeAudit{},
		Outbox: fakeOutbox{},
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/documents/"+docID+"/review",
		`{"status":"approved","notes":"looks good"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestCloseAnomaly_OK_204(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Anomaly: &fakeAnomaly{},
		Audit:   fakeAudit{},
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/close", `{"notes":"resolved"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// vendorAnomaly returns a vendor-targeted open anomaly so triage governance
// (warn/suspend) branches are reachable.
func vendorAnomaly() *fakeAnomaly {
	return &fakeAnomaly{
		getByID: func(_ context.Context, id string) (*compliance.Anomaly, error) {
			return &compliance.Anomaly{
				ID: id, Kind: "on_time_rate_drop", TargetKind: "vendor", TargetID: vendorID,
				Severity: compliance.SeverityHigh, Status: compliance.AnomalyOpen,
			}, nil
		},
	}
}

func TestTriageAnomaly_OK_204(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Anomaly: vendorAnomaly(),
		Audit:   fakeAudit{},
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage", `{"notes":"acknowledged"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestTriageAnomaly_Warn_OK_204(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Anomaly: vendorAnomaly(),
		Audit:   fakeAudit{}, // second WriteTx (vendor.warning) also no-ops
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage",
		`{"notes":"first offence","action":"warn"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestTriageAnomaly_Suspend_OK_204(t *testing.T) {
	gov := &fakeSuspender{}
	svc := &compliance.Service{
		Pool:      fakeBeginner{},
		Anomaly:   vendorAnomaly(),
		Audit:     fakeAudit{},
		VendorGov: gov,
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage",
		`{"notes":"repeat offender","action":"suspend"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.True(t, gov.called, "suspend action should invoke the vendor-governance suspender")
}

func TestTriageAnomaly_Suspend_AlreadyInactive_204(t *testing.T) {
	// Suspending an already non-approved vendor surfaces vendor.ErrInvalidStatus,
	// which the service treats as success (goal already met).
	gov := &fakeSuspender{err: vendor.ErrInvalidStatus}
	svc := &compliance.Service{
		Pool:      fakeBeginner{},
		Anomaly:   vendorAnomaly(),
		Audit:     fakeAudit{},
		VendorGov: gov,
	}
	srv := buildHandler(t, adminUser(), svc)
	resp := do(t, http.MethodPost, srv.URL+"/api/admin/anomalies/"+anomID+"/triage",
		`{"notes":"already suspended","action":"suspend"}`)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.True(t, gov.called)
}
