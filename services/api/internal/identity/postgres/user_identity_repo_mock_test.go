package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listByUserColumns mirrors the SELECT in ListByUser.
var listByUserColumns = []string{"id", "user_id", "provider", "external_subject", "raw_claims", "linked_at"}

// TestListByUser_RowScanError covers the rows.Scan error leg inside the
// for-rows.Next() loop. A string is supplied for the linked_at column whose
// Scan destination is *time.Time, which pgxmock cannot assign/convert, so Scan
// returns an error.
func TestListByUser_RowScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	rows := mock.NewRows(listByUserColumns).
		AddRow("id-1", "user-1", "authentik", "sub-1", []byte(`{}`), "not-a-time")
	mock.ExpectQuery("SELECT id, user_id, provider").
		WithArgs("user-1").
		WillReturnRows(rows)

	repo := &UserIdentityRepo{pool: mock}
	_, err = repo.ListByUser(context.Background(), "user-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_identity row")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListByUser_RowsErr covers the rows.Err() error leg after the loop.
func TestListByUser_RowsErr(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// A clean row scans successfully; CloseError surfaces only via rows.Err()
	// after Next() exhausts the set, exercising the post-loop error leg.
	rows := mock.NewRows(listByUserColumns).
		AddRow("id-1", "user-1", "authentik", "sub-1", []byte(`{}`), time.Now()).
		CloseError(errors.New("boom"))
	mock.ExpectQuery("SELECT id, user_id, provider").
		WithArgs("user-1").
		WillReturnRows(rows)

	repo := &UserIdentityRepo{pool: mock}
	_, err = repo.ListByUser(context.Background(), "user-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_identity rows")
	assert.NoError(t, mock.ExpectationsWereMet())
}
