package compliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
)

// Clock lets tests pin "now".
type Clock interface{ Now() time.Time }

// AuditTx mirrors the audit-repo shape used by order/payroll services so the
// same postgres impl serves compliance writes.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxTx mirrors the outbox-repo shape used by order/payroll services.
type OutboxTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// AuditQuery is the minimal read-side interface for /api/admin/audit.
type AuditQuery interface {
	List(ctx context.Context, filter AuditFilter) ([]AuditRow, error)
}

// AuditFilter narrows an audit-log query. Zero-values mean "no filter".
type AuditFilter struct {
	TargetKind string
	TargetID   string
	Since      time.Time
	Limit      int
}

// AuditRow is a single row returned by AuditQuery.List.
type AuditRow struct {
	ID         int64
	ActorID    *string
	ActorRole  *string
	Action     string
	TargetKind string
	TargetID   string
	Payload    map[string]any
	At         time.Time
	RequestID  string
}

// Service orchestrates vendor document lifecycle, anomaly triage, and audit
// query. Document upload writes blob to S3 then row to DB then audit row.
// Review and anomaly transitions emit outbox events + audit rows in one tx.
type Service struct {
	Pool     *pgxpool.Pool
	Docs     DocumentRepository
	Anomaly  AnomalyRepository
	Storage  *storage.S3Client
	Audit    AuditTx
	Outbox   OutboxTx
	AuditQry AuditQuery
	Clock    Clock
}

// UploadInput captures everything needed to upload+register a vendor doc.
type UploadInput struct {
	VendorID   string
	Kind       DocumentKind
	Filename   string
	Body       io.Reader
	ExpiresAt  *time.Time
	UploadedBy string
}

// UploadDocument streams Body to S3 at vendor-docs/{vendor}/{ts}-{name},
// then inserts a pending vendor_document row and emits an audit event.
// Body is buffered fully so size + audit reflect the same bytes uploaded.
func (s *Service) UploadDocument(ctx context.Context, in UploadInput) (*Document, error) {
	buf, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	key := fmt.Sprintf("vendor-docs/%s/%d-%s", in.VendorID, s.Clock.Now().UnixNano(), in.Filename)
	uri, err := s.Storage.PutObject(ctx, key, bytes.NewReader(buf), "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	uploadedBy := in.UploadedBy
	d := &Document{
		VendorID:   in.VendorID,
		Kind:       in.Kind,
		BlobURI:    uri,
		Filename:   in.Filename,
		UploadedBy: &uploadedBy,
		ExpiresAt:  in.ExpiresAt,
		Status:     DocStatusPending,
	}
	if err := s.Docs.Create(ctx, d); err != nil {
		return nil, err
	}

	role := "welfare_admin"
	auditErr := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		payload := map[string]any{
			"vendor_id":  in.VendorID,
			"kind":       string(in.Kind),
			"filename":   in.Filename,
			"uri":        uri,
			"size_bytes": len(buf),
		}
		return s.Audit.WriteTx(ctx, tx, &uploadedBy, &role, "vendor_document.upload", "vendor_document", d.ID, payload, "")
	})
	if auditErr != nil {
		return nil, auditErr
	}
	return d, nil
}

// ReviewDocument transitions a pending document to approved/rejected and
// emits vendor.document_reviewed.v1 + audit in one transaction.
func (s *Service) ReviewDocument(ctx context.Context, docID, reviewerID string, status DocumentStatus, notes string) error {
	if status != DocStatusApproved && status != DocStatusRejected {
		return ErrInvalidStatus
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Docs.UpdateStatus(ctx, docID, status, &reviewerID, notes); err != nil {
			return err
		}
		role := "welfare_admin"
		payload := map[string]any{
			"document_id": docID,
			"status":      string(status),
			"notes":       notes,
		}
		if err := s.Outbox.AppendTx(ctx, tx, "vendor_document", docID, "vendor.document_reviewed.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.Audit.WriteTx(ctx, tx, &reviewerID, &role, "vendor_document.review", "vendor_document", docID, payload, "")
	})
}

// ListVendorDocuments lists documents for a vendor; includeAll surfaces
// expired rows alongside the live ones.
func (s *Service) ListVendorDocuments(ctx context.Context, vendorID string, includeAll bool) ([]*Document, error) {
	return s.Docs.ListByVendor(ctx, vendorID, includeAll)
}

// OpenAnomaly is the worker-facing path; idempotent via dedup index.
func (s *Service) OpenAnomaly(ctx context.Context, a *Anomaly) error {
	return s.Anomaly.Open(ctx, a)
}

// TriageAnomaly marks an open anomaly as triaged + writes an audit row.
func (s *Service) TriageAnomaly(ctx context.Context, id, by, notes string) error {
	if err := s.Anomaly.Triage(ctx, id, by, notes); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		role := "welfare_admin"
		payload := map[string]any{"anomaly_id": id, "notes": notes}
		return s.Audit.WriteTx(ctx, tx, &by, &role, "anomaly.triage", "anomaly_alert", id, payload, "")
	})
}

// CloseAnomaly closes an open/triaged anomaly + writes an audit row.
func (s *Service) CloseAnomaly(ctx context.Context, id, by, notes string) error {
	if err := s.Anomaly.Close(ctx, id, by, notes); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		role := "welfare_admin"
		payload := map[string]any{"anomaly_id": id, "notes": notes}
		return s.Audit.WriteTx(ctx, tx, &by, &role, "anomaly.close", "anomaly_alert", id, payload, "")
	})
}

// ListAnomalies returns anomalies filtered by status/severity (any/any when nil).
func (s *Service) ListAnomalies(ctx context.Context, statuses []AnomalyStatus, severities []AnomalySeverity) ([]*Anomaly, error) {
	return s.Anomaly.List(ctx, statuses, severities)
}

// GetAnomaly fetches a single anomaly by ID.
func (s *Service) GetAnomaly(ctx context.Context, id string) (*Anomaly, error) {
	return s.Anomaly.GetByID(ctx, id)
}

// QueryAudit returns audit rows matching filter; requires AuditQry wired.
func (s *Service) QueryAudit(ctx context.Context, filter AuditFilter) ([]AuditRow, error) {
	if s.AuditQry == nil {
		return nil, errors.New("compliance: audit query not wired")
	}
	return s.AuditQry.List(ctx, filter)
}
