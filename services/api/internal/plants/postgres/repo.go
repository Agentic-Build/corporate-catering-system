package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
)

type PlantRepo struct{ pool *pgxpool.Pool }

func NewPlantRepo(p *pgxpool.Pool) *PlantRepo { return &PlantRepo{pool: p} }

func (r *PlantRepo) List(ctx context.Context, activeOnly bool) ([]*plants.Plant, error) {
	q := `SELECT code, label, address, active, sort_order, created_at, updated_at
	        FROM plant`
	if activeOnly {
		q += ` WHERE active = true`
	}
	q += ` ORDER BY sort_order, code`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*plants.Plant
	for rows.Next() {
		var p plants.Plant
		if err := rows.Scan(&p.Code, &p.Label, &p.Address, &p.Active, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (r *PlantRepo) Get(ctx context.Context, code string) (*plants.Plant, error) {
	var p plants.Plant
	err := r.pool.QueryRow(ctx, `
SELECT code, label, address, active, sort_order, created_at, updated_at
  FROM plant WHERE code = $1`, code).
		Scan(&p.Code, &p.Label, &p.Address, &p.Active, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, plants.ErrPlantNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PlantRepo) Create(ctx context.Context, p *plants.Plant) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO plant (code, label, address, active, sort_order)
VALUES ($1, $2, $3, $4, $5)`,
		p.Code, p.Label, p.Address, p.Active, p.SortOrder)
	if err != nil {
		if isPKConflict(err) {
			return fmt.Errorf("%w: %s", plants.ErrDuplicateCode, p.Code)
		}
		return err
	}
	return nil
}

func (r *PlantRepo) Update(ctx context.Context, p *plants.Plant) error {
	tag, err := r.pool.Exec(ctx, `
UPDATE plant SET label=$2, address=$3, active=$4, sort_order=$5, updated_at=now()
 WHERE code=$1`,
		p.Code, p.Label, p.Address, p.Active, p.SortOrder)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return plants.ErrPlantNotFound
	}
	return nil
}

func isPKConflict(err error) bool {
	return err != nil && (contains(err.Error(), "duplicate key") || contains(err.Error(), "unique constraint"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexString(s, sub) >= 0)
}

func indexString(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
