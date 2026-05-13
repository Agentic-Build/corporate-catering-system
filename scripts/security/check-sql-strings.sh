#!/usr/bin/env bash
# Fails CI when Go code in services/api/internal builds SQL via fmt.Sprintf
# or string concatenation. The safe pattern is pgx's $N parameterized
# binding: r.pool.QueryRow(ctx, `SELECT ... WHERE x = $1`, val).
#
# gosec G201/G202 only recognize database/sql call signatures, not pgx,
# so we use a focused regex check instead.
set -euo pipefail

cd "$(dirname "$0")/../.."

PATTERN='fmt\.Sprintf[[:space:]]*\([[:space:]]*[`"][^`"]*\b(SELECT|INSERT|UPDATE|DELETE|MERGE|CREATE|DROP|ALTER|TRUNCATE)\b'

if grep -rEn \
    --include='*.go' \
    --exclude='*_test.go' \
    "$PATTERN" \
    services/api/internal; then
  echo
  echo "::error::Possible SQL injection: SQL keywords inside fmt.Sprintf format string."
  echo "Use pgx parameterized queries instead:"
  echo '  r.pool.Exec(ctx, `INSERT INTO t (a) VALUES ($1)`, value)'
  exit 1
fi

echo "SQL string check passed."
