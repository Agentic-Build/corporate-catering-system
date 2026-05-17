package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
)

// OrderReader reads the minimal order projection feedback needs to validate
// ownership and status. It queries the "order" table directly rather than
// depending on the full order aggregate.
type OrderReader struct{ pool *pgxpool.Pool }

func NewOrderReader(p *pgxpool.Pool) *OrderReader { return &OrderReader{pool: p} }

func (r *OrderReader) GetOrderInfo(ctx context.Context, id string) (*feedback.OrderInfo, error) {
	var o feedback.OrderInfo
	err := r.pool.QueryRow(ctx, `
SELECT id, user_id, vendor_id, status::text FROM "order" WHERE id=$1`, id).Scan(
		&o.ID, &o.UserID, &o.VendorID, &o.Status,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, feedback.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order info: %w", err)
	}
	return &o, nil
}
