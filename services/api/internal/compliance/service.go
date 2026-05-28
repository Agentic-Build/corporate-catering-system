package compliance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// txBeginner is the transaction-starting surface of *pgxpool.Pool.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// objectStore is the blob-write surface of *storage.S3Client.
type objectStore interface {
	PutObject(ctx context.Context, key string, body io.Reader, contentType string) (string, error)
}

// VendorSuspender lets anomaly governance suspend a vendor.
type VendorSuspender interface {
	Suspend(ctx context.Context, vendorID, adminUserID string) error
}

// Clock lets tests pin "now".
type Clock interface{ Now() time.Time }

// AuditTx mirrors the audit-repo shape used by order/payroll services.
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
// query. Review/anomaly transitions emit outbox + audit rows in one tx.
type Service struct {
	Pool     txBeginner
	Docs     DocumentRepository
	Anomaly  AnomalyRepository
	Storage  objectStore
	Audit    AuditTx
	Outbox   OutboxTx
	AuditQry AuditQuery
	Clock    Clock
	// Vendors backs the merchant compliance self-view (GET /api/merchant/compliance).
	Vendors VendorReader
	// VendorGov executes anomaly-triage governance actions (suspend). Optional;
	// when nil a "suspend" action is recorded in audit but not carried out.
	VendorGov VendorSuspender
}

// UploadInput captures everything needed to upload+register a vendor doc.
type UploadInput struct {
	VendorID   string
	Kind       DocumentKind
	Filename   string
	Body       io.Reader
	ExpiresAt  *time.Time
	UploadedBy string
	// ActorRole is the audit actor role; defaults to "welfare_admin" when empty
	// (admin upload). Merchant self-service resupply passes "vendor_operator".
	ActorRole string
	// Supersedes, when set, marks this upload as a resupply replacing an
	// existing document of the same vendor.
	Supersedes *string
}

// UploadDocument streams Body to S3 at vendor-docs/{vendor}/{ts}-{name}, then
// inserts a pending vendor_document row and emits an audit event. Body is
// buffered fully so size + audit reflect the same bytes uploaded. When
// in.Supersedes is set, the target must belong to the same vendor and already
// be reviewed; the new row links back via vendor_document.supersedes.
const maxDocumentBytes int64 = 10 << 20

// sanitizeDocFilename strips path components so e.g. "../../payroll/x.csv"
// can't escape the vendor-docs/{vendor}/ key prefix.
func sanitizeDocFilename(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", ErrInvalidFilename
	}
	base := path.Base(trimmed)
	if base == "." || base == ".." || base == "/" {
		return "", ErrInvalidFilename
	}
	return base, nil
}

func (s *Service) UploadDocument(ctx context.Context, in UploadInput) (*Document, error) {
	if in.Supersedes != nil {
		target, err := s.Docs.GetByID(ctx, *in.Supersedes)
		if err != nil {
			return nil, err
		}
		if err := validateResupplyTarget(target, in.VendorID); err != nil {
			return nil, err
		}
	}
	filename, err := sanitizeDocFilename(in.Filename)
	if err != nil {
		return nil, err
	}
	buf, err := io.ReadAll(io.LimitReader(in.Body, maxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(buf)) > maxDocumentBytes {
		return nil, ErrFileTooLarge
	}
	key := fmt.Sprintf("vendor-docs/%s/%d-%s", in.VendorID, s.Clock.Now().UnixNano(), filename)
	uri, err := s.Storage.PutObject(ctx, key, bytes.NewReader(buf), "application/octet-stream")
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	uploadedBy := in.UploadedBy
	d := &Document{
		VendorID:   in.VendorID,
		Kind:       in.Kind,
		BlobURI:    uri,
		Filename:   filename,
		UploadedBy: &uploadedBy,
		ExpiresAt:  in.ExpiresAt,
		Status:     DocStatusPending,
		Supersedes: in.Supersedes,
	}
	role := in.ActorRole
	if role == "" {
		role = "welfare_admin"
	}
	// Row insert + audit in one tx so a failed audit can't leave an orphan
	// document row. Orphan blobs (S3 write is outside the tx) are handled by
	// storage reconciliation.
	auditErr := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Docs.CreateTx(ctx, tx, d); err != nil {
			return err
		}
		payload := map[string]any{
			"vendor_id":  in.VendorID,
			"kind":       string(in.Kind),
			"filename":   filename,
			"uri":        uri,
			"size_bytes": len(buf),
		}
		if in.Supersedes != nil {
			payload["supersedes"] = *in.Supersedes
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
		if err := s.Docs.UpdateStatusTx(ctx, tx, docID, status, &reviewerID, notes); err != nil {
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
	if err := s.Anomaly.Open(ctx, a); err != nil {
		return err
	}
	vendorID := ""
	if a.TargetKind == "vendor" {
		vendorID = a.TargetID
	}
	observability.RecordComplianceViolation(ctx, a.Kind, string(a.Severity), vendorID)
	return nil
}

// Governance actions a welfare admin may attach when triaging an anomaly.
const (
	ActionNone    = ""
	ActionWarn    = "warn"
	ActionSuspend = "suspend"
)

// TriageAnomaly marks an open anomaly as triaged, writes an audit row, and
// optionally carries out a governance action against the anomaly's target
// vendor: "warn" records a warning, "suspend" suspends the vendor. Suspending
// an already non-approved vendor is treated as a no-op success.
func (s *Service) TriageAnomaly(ctx context.Context, id, by, notes, action string) error {
	if action != ActionNone && action != ActionWarn && action != ActionSuspend {
		return ErrInvalidAction
	}
	a, err := s.Anomaly.GetByID(ctx, id)
	if err != nil {
		return err
	}
	role := "welfare_admin"
	auditErr := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Anomaly.TriageTx(ctx, tx, id, by, notes); err != nil {
			return err
		}
		payload := map[string]any{"anomaly_id": id, "notes": notes, "action": action}
		if werr := s.Audit.WriteTx(ctx, tx, &by, &role, "anomaly.triage", "anomaly_alert", id, payload, ""); werr != nil {
			return werr
		}
		if action == ActionWarn && a.TargetKind == "vendor" {
			wp := map[string]any{"anomaly_id": id, "anomaly_kind": a.Kind, "notes": notes}
			return s.Audit.WriteTx(ctx, tx, &by, &role, "vendor.warning", "vendor", a.TargetID, wp, "")
		}
		return nil
	})
	if auditErr != nil {
		return auditErr
	}
	if action == ActionSuspend && a.TargetKind == "vendor" && s.VendorGov != nil {
		// Already-suspended/terminated vendor → ErrInvalidStatus is a no-op success.
		if err := s.VendorGov.Suspend(ctx, a.TargetID, by); err != nil && !errors.Is(err, vendor.ErrInvalidStatus) {
			return err
		}
	}
	return nil
}

// CloseAnomaly closes an open/triaged anomaly + writes an audit row.
func (s *Service) CloseAnomaly(ctx context.Context, id, by, notes string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Anomaly.CloseTx(ctx, tx, id, by, notes); err != nil {
			return err
		}
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
