package postgres

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAnomalyRepo_List_ScanError forces rows.Scan to fail inside the
// for rows.Next() loop (anomaly_repo.go: the Scan error leg), by returning a
// row whose first column (scanned into a string ID) is an incompatible Go type.
func TestAnomalyRepo_List_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := &AnomalyRepo{pool: mock}

	// One row with an int in the id position; id is scanned into *string -> Scan fails.
	rows := pgxmock.NewRows([]string{
		"id", "kind", "target_kind", "target_id", "severity", "status", "payload",
		"evidence_uri", "triaged_at", "triaged_by", "closed_at", "closed_by",
		"notes", "created_at", "updated_at",
	}).AddRow(
		"id", "kind", "vendor", "tid", "medium", "open", []byte("{}"),
		[]string{}, nil, nil, nil, nil, "",
		"not-a-time", nil, // created_at scanned into time.Time -> Scan fails
	)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	out, err := repo.List(context.Background(), nil, nil)
	require.Error(t, err)
	assert.Nil(t, out)
}

// TestDocumentRepo_QueryAll_ScanError forces rows.Scan to fail inside the
// for rows.Next() loop (document_repo.go: the Scan error leg).
func TestDocumentRepo_QueryAll_ScanError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	repo := &DocumentRepo{pool: mock}

	rows := pgxmock.NewRows([]string{
		"id", "vendor_id", "kind", "blob_uri", "filename", "uploaded_by",
		"expires_at", "status", "reviewed_by", "reviewed_at", "notes",
		"supersedes", "created_at", "updated_at",
	}).AddRow(
		"id", "vid", "other", "s3://x", "f", "u",
		nil, "pending", nil, nil, "", nil,
		"not-a-time", nil, // created_at scanned into time.Time -> Scan fails
	)
	mock.ExpectQuery("SELECT").WithArgs("vid").WillReturnRows(rows)

	out, err := repo.ListByVendor(context.Background(), "vid", true)
	require.Error(t, err)
	assert.Nil(t, out)
}
