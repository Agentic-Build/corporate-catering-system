package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
)

type DocumentRepo struct{ pool *pgxpool.Pool }

func NewDocumentRepo(p *pgxpool.Pool) *DocumentRepo { return &DocumentRepo{pool: p} }

const docCols = `id, vendor_id, kind, blob_uri, filename, uploaded_by, expires_at, status, reviewed_by, reviewed_at, notes, supersedes, created_at, updated_at`

func (r *DocumentRepo) Create(ctx context.Context, d *compliance.Document) error {
	status := d.Status
	if status == "" {
		status = compliance.DocStatusPending
	}
	err := r.pool.QueryRow(ctx, `
INSERT INTO vendor_document (vendor_id, kind, blob_uri, filename, uploaded_by, expires_at, status, notes, supersedes)
VALUES ($1, $2::vendor_document_kind, $3, $4, $5, $6, $7::vendor_document_status, $8, $9)
RETURNING id, created_at, updated_at`,
		d.VendorID, string(d.Kind), d.BlobURI, d.Filename, d.UploadedBy, d.ExpiresAt, string(status), d.Notes, d.Supersedes,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create document: %w", err)
	}
	d.Status = status
	return nil
}

func (r *DocumentRepo) GetByID(ctx context.Context, id string) (*compliance.Document, error) {
	return r.scanOne(ctx, `WHERE id=$1`, id)
}

func (r *DocumentRepo) scanOne(ctx context.Context, where string, args ...any) (*compliance.Document, error) {
	var d compliance.Document
	var kind, status string
	q := `SELECT ` + docCols + ` FROM vendor_document ` + where
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&d.ID, &d.VendorID, &kind, &d.BlobURI, &d.Filename, &d.UploadedBy,
		&d.ExpiresAt, &status, &d.ReviewedBy, &d.ReviewedAt, &d.Notes,
		&d.Supersedes, &d.CreatedAt, &d.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, compliance.ErrDocumentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan doc: %w", err)
	}
	d.Kind = compliance.DocumentKind(kind)
	d.Status = compliance.DocumentStatus(status)
	return &d, nil
}

func (r *DocumentRepo) ListByVendor(ctx context.Context, vendorID string, includeAll bool) ([]*compliance.Document, error) {
	where := `WHERE vendor_id=$1`
	if !includeAll {
		where += ` AND status != 'expired'`
	}
	return r.queryAll(ctx, where+` ORDER BY created_at DESC`, vendorID)
}

func (r *DocumentRepo) UpdateStatus(ctx context.Context, id string, status compliance.DocumentStatus, reviewedBy *string, notes string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vendor_document
SET status=$2::vendor_document_status, reviewed_by=$3, reviewed_at=now(), notes=$4, updated_at=now()
WHERE id=$1`, id, string(status), reviewedBy, notes)
	if err != nil {
		return fmt.Errorf("update document status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return compliance.ErrDocumentNotFound
	}
	return nil
}

func (r *DocumentRepo) ListExpiringBefore(ctx context.Context, before time.Time) ([]*compliance.Document, error) {
	return r.queryAll(ctx, `
WHERE status='approved' AND expires_at IS NOT NULL AND expires_at <= $1
ORDER BY expires_at`, before)
}

func (r *DocumentRepo) ListPastExpiry(ctx context.Context, now time.Time) ([]*compliance.Document, error) {
	return r.queryAll(ctx, `
WHERE status='approved' AND expires_at IS NOT NULL AND expires_at < $1
ORDER BY expires_at`, now)
}

func (r *DocumentRepo) queryAll(ctx context.Context, where string, args ...any) ([]*compliance.Document, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+docCols+` FROM vendor_document `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*compliance.Document
	for rows.Next() {
		var d compliance.Document
		var kind, status string
		if err := rows.Scan(&d.ID, &d.VendorID, &kind, &d.BlobURI, &d.Filename, &d.UploadedBy,
			&d.ExpiresAt, &status, &d.ReviewedBy, &d.ReviewedAt, &d.Notes,
			&d.Supersedes, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		d.Kind = compliance.DocumentKind(kind)
		d.Status = compliance.DocumentStatus(status)
		out = append(out, &d)
	}
	return out, rows.Err()
}
