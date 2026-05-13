// Package chttp wires the compliance Service to huma admin endpoints:
// vendor document upload/list/review, anomaly list/triage/close, audit query,
// and a DLQ replay stub. The actual DLQ table + repo lands in P6 Task 6;
// here the endpoint returns 501 so it appears in OpenAPI and the SPA can
// pin to the final URL shape.
package chttp

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// API exposes compliance admin endpoints. All routes require welfare_admin.
type API struct {
	Svc *compliance.Service
}

// ----- DTOs -----

type documentDTO struct {
	ID         string  `json:"id"`
	VendorID   string  `json:"vendor_id"`
	Kind       string  `json:"kind"`
	Filename   string  `json:"filename"`
	BlobURI    string  `json:"blob_uri"`
	ExpiresAt  *string `json:"expires_at,omitempty"`
	Status     string  `json:"status"`
	UploadedBy *string `json:"uploaded_by,omitempty"`
	ReviewedBy *string `json:"reviewed_by,omitempty"`
	ReviewedAt *string `json:"reviewed_at,omitempty"`
	Notes      string  `json:"notes"`
	CreatedAt  string  `json:"created_at"`
}

type anomalyDTO struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	TargetKind  string         `json:"target_kind"`
	TargetID    string         `json:"target_id"`
	Severity    string         `json:"severity"`
	Status      string         `json:"status"`
	Payload     map[string]any `json:"payload"`
	EvidenceURI []string       `json:"evidence_uri"`
	TriagedAt   *string        `json:"triaged_at,omitempty"`
	ClosedAt    *string        `json:"closed_at,omitempty"`
	Notes       string         `json:"notes"`
	CreatedAt   string         `json:"created_at"`
}

type auditRowDTO struct {
	ID         int64          `json:"id"`
	ActorID    *string        `json:"actor_id,omitempty"`
	ActorRole  *string        `json:"actor_role,omitempty"`
	Action     string         `json:"action"`
	TargetKind string         `json:"target_kind"`
	TargetID   string         `json:"target_id"`
	Payload    map[string]any `json:"payload"`
	At         string         `json:"at"`
	RequestID  string         `json:"request_id"`
}

func docToDTO(d *compliance.Document) documentDTO {
	out := documentDTO{
		ID:        d.ID,
		VendorID:  d.VendorID,
		Kind:      string(d.Kind),
		Filename:  d.Filename,
		BlobURI:   d.BlobURI,
		Status:    string(d.Status),
		Notes:     d.Notes,
		CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
	}
	if d.ExpiresAt != nil {
		s := d.ExpiresAt.UTC().Format("2006-01-02")
		out.ExpiresAt = &s
	}
	if d.UploadedBy != nil {
		s := *d.UploadedBy
		out.UploadedBy = &s
	}
	if d.ReviewedBy != nil {
		s := *d.ReviewedBy
		out.ReviewedBy = &s
	}
	if d.ReviewedAt != nil {
		s := d.ReviewedAt.UTC().Format(time.RFC3339)
		out.ReviewedAt = &s
	}
	return out
}

func anomalyToDTO(a *compliance.Anomaly) anomalyDTO {
	payload := a.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	evidence := a.EvidenceURI
	if evidence == nil {
		evidence = []string{}
	}
	out := anomalyDTO{
		ID:          a.ID,
		Kind:        a.Kind,
		TargetKind:  a.TargetKind,
		TargetID:    a.TargetID,
		Severity:    string(a.Severity),
		Status:      string(a.Status),
		Payload:     payload,
		EvidenceURI: evidence,
		Notes:       a.Notes,
		CreatedAt:   a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.TriagedAt != nil {
		s := a.TriagedAt.UTC().Format(time.RFC3339)
		out.TriagedAt = &s
	}
	if a.ClosedAt != nil {
		s := a.ClosedAt.UTC().Format(time.RFC3339)
		out.ClosedAt = &s
	}
	return out
}

func auditRowToDTO(r compliance.AuditRow) auditRowDTO {
	payload := r.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	return auditRowDTO{
		ID:         r.ID,
		ActorID:    r.ActorID,
		ActorRole:  r.ActorRole,
		Action:     r.Action,
		TargetKind: r.TargetKind,
		TargetID:   r.TargetID,
		Payload:    payload,
		At:         r.At.UTC().Format(time.RFC3339),
		RequestID:  r.RequestID,
	}
}

// ----- Inputs / Outputs -----

type listDocumentsInput struct {
	VendorID   string `path:"vendor_id" format:"uuid"`
	IncludeAll bool   `query:"include_all"`
}

type listDocumentsOutput struct {
	Body struct {
		Items []documentDTO `json:"items"`
	}
}

// uploadDocumentInput accepts base64-encoded body in JSON for P6 simplicity;
// multipart/form-data upload is deferred to P8 hardening.
type uploadDocumentInput struct {
	VendorID string `path:"vendor_id" format:"uuid"`
	Body     struct {
		Kind          string  `json:"kind" enum:"business_license,food_safety_permit,tax_registration,insurance,other"`
		Filename      string  `json:"filename" minLength:"1"`
		ContentBase64 string  `json:"content_base64" minLength:"1"`
		ExpiresAt     *string `json:"expires_at,omitempty"`
	}
}

type uploadDocumentOutput struct {
	Body struct {
		Document documentDTO `json:"document"`
	}
}

type reviewDocumentInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Status string `json:"status" enum:"approved,rejected"`
		Notes  string `json:"notes"`
	}
}

type listAnomaliesInput struct {
	Status   string `query:"status" enum:"open,triaged,closed,"`
	Severity string `query:"severity" enum:"low,medium,high,critical,"`
}

type listAnomaliesOutput struct {
	Body struct {
		Items []anomalyDTO `json:"items"`
	}
}

type anomalyActionInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Notes string `json:"notes"`
	}
}

type listAuditInput struct {
	TargetKind string `query:"target_kind"`
	TargetID   string `query:"target_id"`
	Since      string `query:"since"`
	Limit      int    `query:"limit"`
}

type listAuditOutput struct {
	Body struct {
		Items []auditRowDTO `json:"items"`
	}
}

type dlqReplayInput struct {
	Body struct {
		IDs []string `json:"ids"`
	}
}

type dlqReplayOutput struct {
	Body struct {
		ReplayedCount int `json:"replayed_count"`
	}
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "uploadVendorDocument",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{vendor_id}/documents",
		Summary:       "Upload a vendor compliance document (base64-in-JSON)",
		Tags:          []string{"admin", "compliance"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.upload)

	huma.Register(api, huma.Operation{
		OperationID: "listVendorDocuments",
		Method:      http.MethodGet,
		Path:        "/api/admin/vendors/{vendor_id}/documents",
		Summary:     "List a vendor's compliance documents",
		Tags:        []string{"admin", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.list)

	huma.Register(api, huma.Operation{
		OperationID:   "reviewVendorDocument",
		Method:        http.MethodPost,
		Path:          "/api/admin/documents/{id}/review",
		Summary:       "Approve or reject a pending vendor document",
		Tags:          []string{"admin", "compliance"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.review)

	huma.Register(api, huma.Operation{
		OperationID: "listAnomalies",
		Method:      http.MethodGet,
		Path:        "/api/admin/anomalies",
		Summary:     "List anomaly alerts filtered by status/severity",
		Tags:        []string{"admin", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listAnomalies)

	huma.Register(api, huma.Operation{
		OperationID:   "triageAnomaly",
		Method:        http.MethodPost,
		Path:          "/api/admin/anomalies/{id}/triage",
		Summary:       "Mark an open anomaly as triaged",
		Tags:          []string{"admin", "compliance"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.triage)

	huma.Register(api, huma.Operation{
		OperationID:   "closeAnomaly",
		Method:        http.MethodPost,
		Path:          "/api/admin/anomalies/{id}/close",
		Summary:       "Close an open/triaged anomaly",
		Tags:          []string{"admin", "compliance"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.close)

	huma.Register(api, huma.Operation{
		OperationID: "listAuditEvents",
		Method:      http.MethodGet,
		Path:        "/api/admin/audit",
		Summary:     "Query the append-only audit log",
		Tags:        []string{"admin", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.audit)

	huma.Register(api, huma.Operation{
		OperationID: "replayDLQ",
		Method:      http.MethodPost,
		Path:        "/api/admin/dlq/replay",
		Summary:     "Replay messages from the DLQ (stub — P6 Task 6 wires the table)",
		Tags:        []string{"admin", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.dlqReplay)
}

// ----- Auth -----

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, huma.Error403Forbidden("admin role required")
	}
	return u, nil
}

// ----- Handlers -----

func (a *API) upload(ctx context.Context, in *uploadDocumentInput) (*uploadDocumentOutput, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	body, err := base64.StdEncoding.DecodeString(in.Body.ContentBase64)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid base64 content")
	}
	var expires *time.Time
	if in.Body.ExpiresAt != nil && *in.Body.ExpiresAt != "" {
		t, err := time.ParseInLocation("2006-01-02", *in.Body.ExpiresAt, time.UTC)
		if err != nil {
			return nil, huma.Error400BadRequest("expires_at must be YYYY-MM-DD")
		}
		expires = &t
	}
	d, err := a.Svc.UploadDocument(ctx, compliance.UploadInput{
		VendorID:   in.VendorID,
		Kind:       compliance.DocumentKind(in.Body.Kind),
		Filename:   in.Body.Filename,
		Body:       strings.NewReader(string(body)),
		ExpiresAt:  expires,
		UploadedBy: u.ID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp uploadDocumentOutput
	resp.Body.Document = docToDTO(d)
	return &resp, nil
}

func (a *API) list(ctx context.Context, in *listDocumentsInput) (*listDocumentsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	docs, err := a.Svc.ListVendorDocuments(ctx, in.VendorID, in.IncludeAll)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listDocumentsOutput
	resp.Body.Items = make([]documentDTO, 0, len(docs))
	for _, d := range docs {
		resp.Body.Items = append(resp.Body.Items, docToDTO(d))
	}
	return &resp, nil
}

func (a *API) review(ctx context.Context, in *reviewDocumentInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.ReviewDocument(ctx, in.ID, u.ID, compliance.DocumentStatus(in.Body.Status), in.Body.Notes); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listAnomalies(ctx context.Context, in *listAnomaliesInput) (*listAnomaliesOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	var statuses []compliance.AnomalyStatus
	if in.Status != "" {
		statuses = []compliance.AnomalyStatus{compliance.AnomalyStatus(in.Status)}
	}
	var severities []compliance.AnomalySeverity
	if in.Severity != "" {
		severities = []compliance.AnomalySeverity{compliance.AnomalySeverity(in.Severity)}
	}
	items, err := a.Svc.ListAnomalies(ctx, statuses, severities)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listAnomaliesOutput
	resp.Body.Items = make([]anomalyDTO, 0, len(items))
	for _, x := range items {
		resp.Body.Items = append(resp.Body.Items, anomalyToDTO(x))
	}
	return &resp, nil
}

func (a *API) triage(ctx context.Context, in *anomalyActionInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.TriageAnomaly(ctx, in.ID, u.ID, in.Body.Notes); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) close(ctx context.Context, in *anomalyActionInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.CloseAnomaly(ctx, in.ID, u.ID, in.Body.Notes); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) audit(ctx context.Context, in *listAuditInput) (*listAuditOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	filter := compliance.AuditFilter{
		TargetKind: in.TargetKind,
		TargetID:   in.TargetID,
		Limit:      in.Limit,
	}
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			return nil, huma.Error400BadRequest("since must be RFC3339")
		}
		filter.Since = t
	}
	rows, err := a.Svc.QueryAudit(ctx, filter)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listAuditOutput
	resp.Body.Items = make([]auditRowDTO, 0, len(rows))
	for _, r := range rows {
		resp.Body.Items = append(resp.Body.Items, auditRowToDTO(r))
	}
	return &resp, nil
}

// dlqReplay is a P6 Task 3 stub — the synthetic DLQ table + repo lands in
// P6 Task 6. We still register the endpoint so the OpenAPI surface is stable
// and the admin UI can wire to the final path.
func (a *API) dlqReplay(ctx context.Context, _ *dlqReplayInput) (*dlqReplayOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	return nil, huma.Error501NotImplemented("dlq replay not yet implemented (P6 Task 6)")
}

// mapErr translates compliance sentinels to huma HTTP errors.
func mapErr(err error) error {
	switch {
	case errors.Is(err, compliance.ErrDocumentNotFound),
		errors.Is(err, compliance.ErrAnomalyNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, compliance.ErrInvalidStatus):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
