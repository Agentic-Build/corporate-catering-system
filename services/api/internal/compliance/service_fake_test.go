package compliance_test

import (
	"bytes"
	"context"
	"errors"
	audit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// === Fakes (fake-injection style; no DB needed) ===

// fakeBeginner stands in for *pgxpool.Pool. It hands the write closure a no-op
// pgx.Tx; the repo fakes ignore the tx. Set beginErr to exercise tx-open failure.
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

type fakeDocRepo struct {
	getByID    *compliance.Document
	getErr     error
	createErr  error
	updateErr  error
	listDocs   []*compliance.Document
	listErr    error
	created    *compliance.Document
	lastUpdate struct {
		id     string
		status compliance.DocumentStatus
		notes  string
	}
}

func (r *fakeDocRepo) Create(context.Context, *compliance.Document) error { return nil }
func (r *fakeDocRepo) CreateTx(_ context.Context, _ pgx.Tx, d *compliance.Document) error {
	if r.createErr != nil {
		return r.createErr
	}
	if d.ID == "" {
		d.ID = "doc-new"
	}
	r.created = d
	return nil
}
func (r *fakeDocRepo) GetByID(_ context.Context, _ string) (*compliance.Document, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.getByID, nil
}
func (r *fakeDocRepo) ListByVendor(_ context.Context, _ string, _ bool) ([]*compliance.Document, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listDocs, nil
}
func (r *fakeDocRepo) UpdateStatus(context.Context, string, compliance.DocumentStatus, *string, string) error {
	return nil
}
func (r *fakeDocRepo) UpdateStatusTx(_ context.Context, _ pgx.Tx, id string, status compliance.DocumentStatus, _ *string, notes string) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.lastUpdate.id = id
	r.lastUpdate.status = status
	r.lastUpdate.notes = notes
	return nil
}
func (r *fakeDocRepo) ListExpiringBefore(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}
func (r *fakeDocRepo) ListPastExpiry(context.Context, time.Time) ([]*compliance.Document, error) {
	return nil, nil
}

type fakeAnomalyRepo struct {
	openErr    error
	opened     *compliance.Anomaly
	getByID    *compliance.Anomaly
	getErr     error
	listResult []*compliance.Anomaly
	listErr    error
	closeErr   error
	triageErr  error
}

func (r *fakeAnomalyRepo) Open(_ context.Context, a *compliance.Anomaly) error {
	if r.openErr != nil {
		return r.openErr
	}
	r.opened = a
	return nil
}
func (r *fakeAnomalyRepo) GetByID(_ context.Context, _ string) (*compliance.Anomaly, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.getByID, nil
}
func (r *fakeAnomalyRepo) List(_ context.Context, _ []compliance.AnomalyStatus, _ []compliance.AnomalySeverity) ([]*compliance.Anomaly, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return r.listResult, nil
}
func (r *fakeAnomalyRepo) Triage(context.Context, string, string, string) error { return nil }
func (r *fakeAnomalyRepo) TriageTx(_ context.Context, _ pgx.Tx, _, _, _ string) error {
	return r.triageErr
}
func (r *fakeAnomalyRepo) Close(context.Context, string, string, string) error { return nil }
func (r *fakeAnomalyRepo) CloseTx(_ context.Context, _ pgx.Tx, _, _, _ string) error {
	return r.closeErr
}

type fakeStore struct {
	uri string
	err error
}

func (s fakeStore) PutObject(context.Context, string, io.Reader, string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.uri != "" {
		return s.uri, nil
	}
	return "s3://bucket/object", nil
}

type recordingAudit struct {
	calls []string // actions written
	err   error
}

func (a *recordingAudit) WriteTx(_ context.Context, _ pgx.Tx, e audit.Entry) error {
	if a.err != nil {
		return a.err
	}
	a.calls = append(a.calls, e.Action)
	return nil
}

type recordingOutbox struct {
	subjects []string
	err      error
}

func (o *recordingOutbox) AppendTx(_ context.Context, _ pgx.Tx, _, _, subject string, _, _ map[string]any) error {
	if o.err != nil {
		return o.err
	}
	o.subjects = append(o.subjects, subject)
	return nil
}

type fakeVendorReader struct {
	v   *vendor.Vendor
	err error
}

func (f fakeVendorReader) GetByID(context.Context, string) (*vendor.Vendor, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.v, nil
}

type fakeAuditQuery struct {
	rows []compliance.AuditRow
	err  error
}

func (q fakeAuditQuery) List(context.Context, compliance.AuditFilter) ([]compliance.AuditRow, error) {
	if q.err != nil {
		return nil, q.err
	}
	return q.rows, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

var errBoom = errors.New("boom")

// === UploadDocument ===

func TestUploadDocument_Success(t *testing.T) {
	docs := &fakeDocRepo{}
	audit := &recordingAudit{}
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    docs,
		Storage: fakeStore{uri: "s3://b/k"},
		Audit:   audit,
		Clock:   fixedClock{t: time.Unix(1700000000, 0)},
	}

	d, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID:   "v1",
		Kind:       compliance.DocKindBusinessLicense,
		Filename:   "../../etc/license.pdf",
		Body:       strings.NewReader("hello"),
		UploadedBy: "u1",
	})
	require.NoError(t, err)
	assert.Equal(t, "license.pdf", d.Filename)
	assert.Equal(t, "s3://b/k", d.BlobURI)
	assert.Equal(t, compliance.DocStatusPending, d.Status)
	assert.Equal(t, []string{"vendor_document.upload"}, audit.calls)
}

func TestUploadDocument_InvalidFilename(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{},
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "   ",
		Body: strings.NewReader("x"),
	})
	assert.ErrorIs(t, err, compliance.ErrInvalidFilename)
}

func TestUploadDocument_FileTooLarge(t *testing.T) {
	big := bytes.Repeat([]byte("a"), (10<<20)+1)
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{},
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "big.bin",
		Body: bytes.NewReader(big),
	})
	assert.ErrorIs(t, err, compliance.ErrFileTooLarge)
}

func TestUploadDocument_StorageError(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{},
		Storage: fakeStore{err: errBoom},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "f.pdf",
		Body: strings.NewReader("x"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload:")
}

func TestUploadDocument_AuditError(t *testing.T) {
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    &fakeDocRepo{},
		Storage: fakeStore{},
		Audit:   &recordingAudit{err: errBoom},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindOther, Filename: "f.pdf",
		Body: strings.NewReader("x"),
	})
	assert.ErrorIs(t, err, errBoom)
}

func TestUploadDocument_Resupply_Success(t *testing.T) {
	prior := &compliance.Document{ID: "old", VendorID: "v1", Status: compliance.DocStatusRejected}
	docs := &fakeDocRepo{getByID: prior}
	audit := &recordingAudit{}
	sup := "old"
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    docs,
		Storage: fakeStore{},
		Audit:   audit,
		Clock:   fixedClock{t: time.Now()},
	}
	d, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindBusinessLicense, Filename: "new.pdf",
		Body: strings.NewReader("x"), UploadedBy: "u1", Supersedes: &sup,
	})
	require.NoError(t, err)
	require.NotNil(t, d.Supersedes)
	assert.Equal(t, "old", *d.Supersedes)
}

func TestUploadDocument_Resupply_TargetNotFound(t *testing.T) {
	docs := &fakeDocRepo{getErr: compliance.ErrDocumentNotFound}
	sup := "missing"
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    docs,
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindBusinessLicense, Filename: "new.pdf",
		Body: strings.NewReader("x"), Supersedes: &sup,
	})
	assert.ErrorIs(t, err, compliance.ErrDocumentNotFound)
}

func TestUploadDocument_Resupply_InvalidTarget(t *testing.T) {
	// Target belongs to a different vendor → ErrInvalidResupply.
	prior := &compliance.Document{ID: "old", VendorID: "other", Status: compliance.DocStatusRejected}
	docs := &fakeDocRepo{getByID: prior}
	sup := "old"
	svc := &compliance.Service{
		Pool:    fakeBeginner{},
		Docs:    docs,
		Storage: fakeStore{},
		Audit:   &recordingAudit{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.UploadDocument(context.Background(), compliance.UploadInput{
		VendorID: "v1", Kind: compliance.DocKindBusinessLicense, Filename: "new.pdf",
		Body: strings.NewReader("x"), Supersedes: &sup,
	})
	assert.ErrorIs(t, err, compliance.ErrInvalidResupply)
}

// === ReviewDocument ===

func TestReviewDocument_Approved(t *testing.T) {
	docs := &fakeDocRepo{}
	audit := &recordingAudit{}
	outbox := &recordingOutbox{}
	svc := &compliance.Service{
		Pool:   fakeBeginner{},
		Docs:   docs,
		Audit:  audit,
		Outbox: outbox,
	}
	err := svc.ReviewDocument(context.Background(), "d1", "rev1", compliance.DocStatusApproved, "ok")
	require.NoError(t, err)
	assert.Equal(t, compliance.DocStatusApproved, docs.lastUpdate.status)
	assert.Equal(t, []string{"vendor.document_reviewed.v1"}, outbox.subjects)
	assert.Equal(t, []string{"vendor_document.review"}, audit.calls)
}

func TestReviewDocument_InvalidStatus(t *testing.T) {
	svc := &compliance.Service{Pool: fakeBeginner{}, Docs: &fakeDocRepo{}, Audit: &recordingAudit{}, Outbox: &recordingOutbox{}}
	err := svc.ReviewDocument(context.Background(), "d1", "rev1", compliance.DocStatusPending, "")
	assert.ErrorIs(t, err, compliance.ErrInvalidStatus)
}

func TestReviewDocument_UpdateError(t *testing.T) {
	svc := &compliance.Service{
		Pool:   fakeBeginner{},
		Docs:   &fakeDocRepo{updateErr: compliance.ErrDocumentNotFound},
		Audit:  &recordingAudit{},
		Outbox: &recordingOutbox{},
	}
	err := svc.ReviewDocument(context.Background(), "d1", "rev1", compliance.DocStatusRejected, "no")
	assert.ErrorIs(t, err, compliance.ErrDocumentNotFound)
}

func TestReviewDocument_OutboxError(t *testing.T) {
	svc := &compliance.Service{
		Pool:   fakeBeginner{},
		Docs:   &fakeDocRepo{},
		Audit:  &recordingAudit{},
		Outbox: &recordingOutbox{err: errBoom},
	}
	err := svc.ReviewDocument(context.Background(), "d1", "rev1", compliance.DocStatusApproved, "")
	assert.ErrorIs(t, err, errBoom)
}

func TestReviewDocument_BeginError(t *testing.T) {
	svc := &compliance.Service{
		Pool:   fakeBeginner{beginErr: errBoom},
		Docs:   &fakeDocRepo{},
		Audit:  &recordingAudit{},
		Outbox: &recordingOutbox{},
	}
	err := svc.ReviewDocument(context.Background(), "d1", "rev1", compliance.DocStatusApproved, "")
	assert.ErrorIs(t, err, errBoom)
}

// === ListVendorDocuments ===

func TestListVendorDocuments(t *testing.T) {
	want := []*compliance.Document{{ID: "d1"}, {ID: "d2"}}
	svc := &compliance.Service{Docs: &fakeDocRepo{listDocs: want}}
	got, err := svc.ListVendorDocuments(context.Background(), "v1", true)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestListVendorDocuments_Error(t *testing.T) {
	svc := &compliance.Service{Docs: &fakeDocRepo{listErr: errBoom}}
	_, err := svc.ListVendorDocuments(context.Background(), "v1", false)
	assert.ErrorIs(t, err, errBoom)
}

// === OpenAnomaly ===

func TestOpenAnomaly_Success(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	svc := &compliance.Service{Anomaly: repo}
	a := &compliance.Anomaly{Kind: "late", TargetKind: "vendor", TargetID: "v1", Severity: compliance.SeverityHigh}
	require.NoError(t, svc.OpenAnomaly(context.Background(), a))
	assert.Same(t, a, repo.opened)
}

func TestOpenAnomaly_NonVendorTarget(t *testing.T) {
	repo := &fakeAnomalyRepo{}
	svc := &compliance.Service{Anomaly: repo}
	a := &compliance.Anomaly{Kind: "data", TargetKind: "plant", TargetID: "p1", Severity: compliance.SeverityLow}
	require.NoError(t, svc.OpenAnomaly(context.Background(), a))
}

func TestOpenAnomaly_Error(t *testing.T) {
	svc := &compliance.Service{Anomaly: &fakeAnomalyRepo{openErr: errBoom}}
	err := svc.OpenAnomaly(context.Background(), &compliance.Anomaly{TargetKind: "vendor"})
	assert.ErrorIs(t, err, errBoom)
}

// === CloseAnomaly ===

func TestCloseAnomaly_Success(t *testing.T) {
	audit := &recordingAudit{}
	svc := &compliance.Service{Pool: fakeBeginner{}, Anomaly: &fakeAnomalyRepo{}, Audit: audit}
	require.NoError(t, svc.CloseAnomaly(context.Background(), "an1", "by1", "done"))
	assert.Equal(t, []string{"anomaly.close"}, audit.calls)
}

func TestCloseAnomaly_CloseError(t *testing.T) {
	svc := &compliance.Service{Pool: fakeBeginner{}, Anomaly: &fakeAnomalyRepo{closeErr: compliance.ErrAnomalyNotFound}, Audit: &recordingAudit{}}
	err := svc.CloseAnomaly(context.Background(), "an1", "by1", "done")
	assert.ErrorIs(t, err, compliance.ErrAnomalyNotFound)
}

func TestCloseAnomaly_AuditError(t *testing.T) {
	svc := &compliance.Service{Pool: fakeBeginner{}, Anomaly: &fakeAnomalyRepo{}, Audit: &recordingAudit{err: errBoom}}
	err := svc.CloseAnomaly(context.Background(), "an1", "by1", "done")
	assert.ErrorIs(t, err, errBoom)
}

// === ListAnomalies / GetAnomaly ===

func TestListAnomalies(t *testing.T) {
	want := []*compliance.Anomaly{{ID: "a1"}}
	svc := &compliance.Service{Anomaly: &fakeAnomalyRepo{listResult: want}}
	got, err := svc.ListAnomalies(context.Background(), []compliance.AnomalyStatus{compliance.AnomalyOpen}, nil)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestListAnomalies_Error(t *testing.T) {
	svc := &compliance.Service{Anomaly: &fakeAnomalyRepo{listErr: errBoom}}
	_, err := svc.ListAnomalies(context.Background(), nil, nil)
	assert.ErrorIs(t, err, errBoom)
}

// === QueryAudit ===

func TestQueryAudit_NotWired(t *testing.T) {
	svc := &compliance.Service{}
	_, err := svc.QueryAudit(context.Background(), compliance.AuditFilter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audit query not wired")
}

func TestQueryAudit_Success(t *testing.T) {
	want := []compliance.AuditRow{{ID: 1, Action: "x"}}
	svc := &compliance.Service{AuditQry: fakeAuditQuery{rows: want}}
	got, err := svc.QueryAudit(context.Background(), compliance.AuditFilter{Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestQueryAudit_Error(t *testing.T) {
	svc := &compliance.Service{AuditQry: fakeAuditQuery{err: errBoom}}
	_, err := svc.QueryAudit(context.Background(), compliance.AuditFilter{})
	assert.ErrorIs(t, err, errBoom)
}

// === MerchantCompliance ===

func TestMerchantCompliance_Success(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -1)
	docs := []*compliance.Document{
		{Kind: compliance.DocKindBusinessLicense, Status: compliance.DocStatusApproved, ExpiresAt: &past},
	}
	svc := &compliance.Service{
		Vendors: fakeVendorReader{v: &vendor.Vendor{ID: "v1", DisplayName: "Acme", Status: vendor.StatusApproved}},
		Docs:    &fakeDocRepo{listDocs: docs},
		Clock:   fixedClock{t: now},
	}
	got, err := svc.MerchantCompliance(context.Background(), "v1")
	require.NoError(t, err)
	assert.Equal(t, "v1", got.Vendor.ID)
	assert.Equal(t, "Acme", got.Vendor.DisplayName)
	assert.Equal(t, "approved", got.Vendor.Status)
	assert.Equal(t, docs, got.Documents)
	// business_license expired + 3 other required kinds missing.
	require.NotEmpty(t, got.Warnings)
}

func TestMerchantCompliance_VendorError(t *testing.T) {
	svc := &compliance.Service{
		Vendors: fakeVendorReader{err: vendor.ErrInvalidStatus},
		Docs:    &fakeDocRepo{},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.MerchantCompliance(context.Background(), "v1")
	assert.ErrorIs(t, err, vendor.ErrInvalidStatus)
}

func TestMerchantCompliance_DocsError(t *testing.T) {
	svc := &compliance.Service{
		Vendors: fakeVendorReader{v: &vendor.Vendor{ID: "v1"}},
		Docs:    &fakeDocRepo{listErr: errBoom},
		Clock:   fixedClock{t: time.Now()},
	}
	_, err := svc.MerchantCompliance(context.Background(), "v1")
	assert.ErrorIs(t, err, errBoom)
}
