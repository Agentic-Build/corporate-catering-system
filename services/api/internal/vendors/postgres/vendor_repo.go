package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

type VendorRepo struct{ pool *pgxpool.Pool }

func NewVendorRepo(p *pgxpool.Pool) *VendorRepo { return &VendorRepo{pool: p} }

func (r *VendorRepo) Create(ctx context.Context, v *vendor.Vendor) error {
	return r.pool.QueryRow(ctx, `
INSERT INTO vendor (display_name, legal_name, contact_email, status)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at, updated_at`,
		v.DisplayName, v.LegalName, v.ContactEmail, string(v.Status),
	).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
}

func (r *VendorRepo) GetByID(ctx context.Context, id string) (*vendor.Vendor, error) {
	return r.one(ctx, `WHERE id = $1`, id)
}

func (r *VendorRepo) GetByEmail(ctx context.Context, email string) (*vendor.Vendor, error) {
	return r.one(ctx, `WHERE contact_email = $1`, email)
}

func (r *VendorRepo) one(ctx context.Context, where string, args ...any) (*vendor.Vendor, error) {
	var v vendor.Vendor
	var status string
	q := `SELECT id, display_name, legal_name, contact_email, status, approved_at, approved_by, cutoff_hour, preorder_window_days, created_at, updated_at FROM vendor ` + where
	err := r.pool.QueryRow(ctx, q, args...).Scan(
		&v.ID, &v.DisplayName, &v.LegalName, &v.ContactEmail, &status,
		&v.ApprovedAt, &v.ApprovedBy, &v.CutoffHour, &v.PreorderWindowDays, &v.CreatedAt, &v.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, vendor.ErrVendorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("vendor scan: %w", err)
	}
	v.Status = vendor.Status(status)
	return &v, nil
}

func (r *VendorRepo) UpdateStatus(ctx context.Context, id string, status vendor.Status, approvedBy *string) error {
	if status == vendor.StatusApproved {
		_, err := r.pool.Exec(ctx, `UPDATE vendor SET status=$2, approved_at=now(), approved_by=$3, updated_at=now() WHERE id=$1`, id, string(status), approvedBy)
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE vendor SET status=$2, updated_at=now() WHERE id=$1`, id, string(status))
	return err
}

// UpdateSettings updates the per-vendor ordering settings.
func (r *VendorRepo) UpdateSettings(ctx context.Context, id string, cutoffHour, preorderWindowDays int) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vendor SET cutoff_hour=$2, preorder_window_days=$3, updated_at=now() WHERE id=$1`,
		id, cutoffHour, preorderWindowDays)
	if err != nil {
		return fmt.Errorf("update vendor settings: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return vendor.ErrVendorNotFound
	}
	return nil
}

// UpdateContactEmail updates the vendor's contact email.
func (r *VendorRepo) UpdateContactEmail(ctx context.Context, id, email string) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE vendor SET contact_email=$2, updated_at=now() WHERE id=$1`, id, email)
	if err != nil {
		return fmt.Errorf("update vendor contact email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return vendor.ErrVendorNotFound
	}
	return nil
}

func (r *VendorRepo) List(ctx context.Context, statuses []vendor.Status) ([]*vendor.Vendor, error) {
	args := []any{}
	where := ""
	if len(statuses) > 0 {
		placeholders := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		}
		where = "WHERE status IN (" + strings.Join(placeholders, ",") + ")"
	}
	q := `SELECT id, display_name, legal_name, contact_email, status, approved_at, approved_by, cutoff_hour, preorder_window_days, created_at, updated_at FROM vendor ` + where + ` ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*vendor.Vendor
	for rows.Next() {
		var v vendor.Vendor
		var status string
		if err := rows.Scan(&v.ID, &v.DisplayName, &v.LegalName, &v.ContactEmail, &status,
			&v.ApprovedAt, &v.ApprovedBy, &v.CutoffHour, &v.PreorderWindowDays, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		v.Status = vendor.Status(status)
		out = append(out, &v)
	}
	return out, rows.Err()
}
