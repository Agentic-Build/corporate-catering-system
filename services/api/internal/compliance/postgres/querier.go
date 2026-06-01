package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgxQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, letting a repo
// method run standalone or inside a caller-owned transaction.
type pgxQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// queryer is the subset of *pgxpool.Pool used by the repos in this package.
// Declaring it as a seam lets tests drive the repos with a pgxmock pool.
// Both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy it.
type queryer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
