package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
)

type ComplaintRepo struct{ pool *pgxpool.Pool }

func NewComplaintRepo(p *pgxpool.Pool) *ComplaintRepo { return &ComplaintRepo{pool: p} }

const complaintCols = `id, order_id, user_id, vendor_id, category, description, status,
       vendor_response, vendor_responded_at, escalated_at,
       resolution, resolved_by, resolved_at, created_at, updated_at`

func (r *ComplaintRepo) CreateTx(ctx context.Context, tx pgx.Tx, c *feedback.Complaint) error {
	if tx == nil {
		return errors.New("ComplaintRepo.CreateTx requires a tx")
	}
	status := c.Status
	if status == "" {
		status = feedback.StatusOpen
	}
	err := tx.QueryRow(ctx, `
INSERT INTO meal_complaint (order_id, user_id, vendor_id, category, description, status)
VALUES ($1, $2, $3, $4::meal_complaint_category, $5, $6::meal_complaint_status)
RETURNING id, created_at, updated_at`,
		c.OrderID, c.UserID, c.VendorID, string(c.Category), c.Description, string(status),
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create complaint: %w", err)
	}
	c.Status = status
	return nil
}

func (r *ComplaintRepo) GetByID(ctx context.Context, id string) (*feedback.Complaint, error) {
	var c feedback.Complaint
	var category, status string
	err := r.pool.QueryRow(ctx, `SELECT `+complaintCols+` FROM meal_complaint WHERE id=$1`, id).Scan(
		&c.ID, &c.OrderID, &c.UserID, &c.VendorID, &category, &c.Description, &status,
		&c.VendorResponse, &c.VendorRespondedAt, &c.EscalatedAt,
		&c.Resolution, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, feedback.ErrComplaintNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan complaint: %w", err)
	}
	c.Category = feedback.ComplaintCategory(category)
	c.Status = feedback.ComplaintStatus(status)
	return &c, nil
}

// UpdateStatusTx applies a conditional status transition. The SET clause writes
// the workflow timestamps that correspond to the target status so callers do
// not need to pass them explicitly.
func (r *ComplaintRepo) UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to feedback.ComplaintStatus, fields feedback.ComplaintUpdate) error {
	if tx == nil {
		return errors.New("ComplaintRepo.UpdateStatusTx requires a tx")
	}
	tag, err := tx.Exec(ctx, `
UPDATE meal_complaint
   SET status=$3::meal_complaint_status,
       vendor_response = CASE WHEN $3 = 'vendor_responded' THEN $4 ELSE vendor_response END,
       vendor_responded_at = CASE WHEN $3 = 'vendor_responded' THEN now() ELSE vendor_responded_at END,
       escalated_at = CASE WHEN $3 = 'escalated' THEN now() ELSE escalated_at END,
       resolution = CASE WHEN $3 = 'resolved' THEN $5 ELSE resolution END,
       resolved_by = CASE WHEN $3 = 'resolved' THEN $6 ELSE resolved_by END,
       resolved_at = CASE WHEN $3 = 'resolved' THEN now() ELSE resolved_at END,
       updated_at = now()
 WHERE id=$1 AND status=$2::meal_complaint_status`,
		id, string(from), string(to), fields.VendorResponse, fields.Resolution, fields.ResolvedBy)
	if err != nil {
		return fmt.Errorf("update complaint status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return feedback.ErrInvalidTransition
	}
	return nil
}

func (r *ComplaintRepo) ListByUser(ctx context.Context, userID string) ([]*feedback.Complaint, error) {
	rows, err := r.pool.Query(ctx, `
SELECT `+complaintCols+` FROM meal_complaint WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return collectComplaints(rows)
}

func (r *ComplaintRepo) ListByVendor(ctx context.Context, vendorID string, statuses []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	args := []any{vendorID}
	where := "WHERE vendor_id=$1"
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::meal_complaint_status", len(args))
		}
		where += " AND status IN (" + strings.Join(ph, ",") + ")"
	}
	rows, err := r.pool.Query(ctx, `SELECT `+complaintCols+` FROM meal_complaint `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	return collectComplaints(rows)
}

func (r *ComplaintRepo) ListByStatus(ctx context.Context, statuses []feedback.ComplaintStatus) ([]*feedback.Complaint, error) {
	args := []any{}
	where := ""
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			args = append(args, string(s))
			ph[i] = fmt.Sprintf("$%d::meal_complaint_status", len(args))
		}
		where = "WHERE status IN (" + strings.Join(ph, ",") + ")"
	}
	rows, err := r.pool.Query(ctx, `SELECT `+complaintCols+` FROM meal_complaint `+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	return collectComplaints(rows)
}

// CountByVendorSince returns per-vendor complaint counts for complaints created
// on or after `since`.
func (r *ComplaintRepo) CountByVendorSince(ctx context.Context, since time.Time) ([]feedback.VendorComplaintStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT vendor_id, count(*)::int
  FROM meal_complaint
 WHERE created_at >= $1
 GROUP BY vendor_id`, since)
	if err != nil {
		return nil, fmt.Errorf("count complaints: %w", err)
	}
	defer rows.Close()
	var out []feedback.VendorComplaintStat
	for rows.Next() {
		var s feedback.VendorComplaintStat
		if err := rows.Scan(&s.VendorID, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func collectComplaints(rows pgx.Rows) ([]*feedback.Complaint, error) {
	defer rows.Close()
	var out []*feedback.Complaint
	for rows.Next() {
		var c feedback.Complaint
		var category, status string
		if err := rows.Scan(
			&c.ID, &c.OrderID, &c.UserID, &c.VendorID, &category, &c.Description, &status,
			&c.VendorResponse, &c.VendorRespondedAt, &c.EscalatedAt,
			&c.Resolution, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		c.Category = feedback.ComplaintCategory(category)
		c.Status = feedback.ComplaintStatus(status)
		out = append(out, &c)
	}
	return out, rows.Err()
}
