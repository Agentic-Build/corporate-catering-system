package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
)

type RatingRepo struct{ pool *pgxpool.Pool }

func NewRatingRepo(p *pgxpool.Pool) *RatingRepo { return &RatingRepo{pool: p} }

const ratingCols = `id, order_id, user_id, vendor_id, score, comment, created_at`

func (r *RatingRepo) CreateTx(ctx context.Context, tx pgx.Tx, m *feedback.Rating) error {
	if tx == nil {
		return errors.New("RatingRepo.CreateTx requires a tx")
	}
	err := tx.QueryRow(ctx, `
INSERT INTO meal_rating (order_id, user_id, vendor_id, score, comment)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at`,
		m.OrderID, m.UserID, m.VendorID, m.Score, m.Comment,
	).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("create rating: %w", err)
	}
	return nil
}

func (r *RatingRepo) GetByOrder(ctx context.Context, orderID string) (*feedback.Rating, error) {
	var m feedback.Rating
	err := r.pool.QueryRow(ctx, `SELECT `+ratingCols+` FROM meal_rating WHERE order_id=$1`, orderID).Scan(
		&m.ID, &m.OrderID, &m.UserID, &m.VendorID, &m.Score, &m.Comment, &m.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, feedback.ErrRatingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan rating: %w", err)
	}
	return &m, nil
}

// AggregateByVendorSince returns per-vendor avg score + sample count for
// ratings created on or after `since`.
func (r *RatingRepo) AggregateByVendorSince(ctx context.Context, since time.Time) ([]feedback.VendorRatingStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT vendor_id, avg(score)::float8, count(*)::int
  FROM meal_rating
 WHERE created_at >= $1
 GROUP BY vendor_id`, since)
	if err != nil {
		return nil, fmt.Errorf("aggregate ratings: %w", err)
	}
	defer rows.Close()
	var out []feedback.VendorRatingStat
	for rows.Next() {
		var s feedback.VendorRatingStat
		if err := rows.Scan(&s.VendorID, &s.AvgScore, &s.SampleCount); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
