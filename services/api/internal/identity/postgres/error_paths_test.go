package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/postgres"
)

// closedPool spins up a real Postgres, runs migrations, then closes the pool so
// every subsequent pool operation (Query/QueryRow) fails deterministically with
// a "closed pool" error. Mirrors the helper used in sibling postgres packages.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, cleanup := setupPostgres(t)
	t.Cleanup(cleanup)
	pool.Close()
	return pool
}

// seedUser inserts a minimal user and returns its id, for FK-valid identities.
func seedUser(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	users := postgres.NewUserRepo(pool)
	u := &identity.User{
		PrimaryEmail: email,
		DisplayName:  "Seed",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
	}
	require.NoError(t, users.Create(context.Background(), u))
	return u.ID
}

// ---- user_identity_repo.go ----

// Link must default a nil RawClaims map to {} (covers the nil-claims branch).
func TestUserIdentityRepo_Link_NilClaims(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "nilclaims@example.com")

	repo := postgres.NewUserIdentityRepo(pool)
	ui := &identity.UserIdentity{
		UserID:          uid,
		Provider:        identity.Provider("authentik"),
		ExternalSubject: "nilclaims-sub",
		RawClaims:       nil,
	}
	require.NoError(t, repo.Link(ctx, ui))
	require.NotEmpty(t, ui.ID)

	got, err := repo.GetByProviderSubject(ctx, identity.Provider("authentik"), "nilclaims-sub")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{}, got.RawClaims)
}

// Link must surface a json.Marshal failure on the raw claims (covers the
// marshal-error branch). A channel value is not JSON-serialisable.
func TestUserIdentityRepo_Link_MarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "marshalerr@example.com")

	repo := postgres.NewUserIdentityRepo(pool)
	err := repo.Link(ctx, &identity.UserIdentity{
		UserID:          uid,
		Provider:        identity.Provider("authentik"),
		ExternalSubject: "marshalerr-sub",
		RawClaims:       map[string]any{"bad": make(chan int)},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal raw_claims")
}

// GetByProviderSubject must wrap a non-NoRows scan error (covers the scan
// error branch). A closed pool fails QueryRow deterministically.
func TestUserIdentityRepo_GetByProviderSubject_ScanError(t *testing.T) {
	pool := closedPool(t)
	repo := postgres.NewUserIdentityRepo(pool)
	_, err := repo.GetByProviderSubject(context.Background(), identity.Provider("authentik"), "x")
	require.Error(t, err)
	assert.NotErrorIs(t, err, identity.ErrIdentityNotFound)
	assert.Contains(t, err.Error(), "user_identity scan")
}

// GetByProviderSubject must surface a json.Unmarshal failure when raw_claims is
// valid JSONB but not a JSON object (covers the unmarshal-error branch).
func TestUserIdentityRepo_GetByProviderSubject_UnmarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "badjson-get@example.com")

	// JSONB stores a top-level array — valid JSONB, but cannot unmarshal into
	// the map[string]any RawClaims field.
	_, err := pool.Exec(ctx, `
INSERT INTO user_identity (user_id, provider, external_subject, raw_claims)
VALUES ($1, $2, $3, '[1,2,3]'::jsonb)`, uid, "authentik", "badjson-get-sub")
	require.NoError(t, err)

	repo := postgres.NewUserIdentityRepo(pool)
	_, err = repo.GetByProviderSubject(ctx, identity.Provider("authentik"), "badjson-get-sub")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal raw_claims")
}

// ListByUser must wrap a query error (covers the query-error branch).
func TestUserIdentityRepo_ListByUser_QueryError(t *testing.T) {
	pool := closedPool(t)
	repo := postgres.NewUserIdentityRepo(pool)
	_, err := repo.ListByUser(context.Background(), "some-user-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user_identity list")
}

// ListByUser must surface a json.Unmarshal failure on a row (covers the
// per-row unmarshal-error branch).
func TestUserIdentityRepo_ListByUser_UnmarshalError(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	uid := seedUser(t, pool, "badjson-list@example.com")

	_, err := pool.Exec(ctx, `
INSERT INTO user_identity (user_id, provider, external_subject, raw_claims)
VALUES ($1, $2, $3, '"not-an-object"'::jsonb)`, uid, "authentik", "badjson-list-sub")
	require.NoError(t, err)

	repo := postgres.NewUserIdentityRepo(pool)
	_, err = repo.ListByUser(ctx, uid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal raw_claims")
}

// ---- user_repo.go ----

// scanOne must wrap a non-NoRows error (covers the err != nil branch distinct
// from ErrUserNotFound).
func TestUserRepo_ScanError(t *testing.T) {
	pool := closedPool(t)
	repo := postgres.NewUserRepo(pool)
	_, err := repo.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	assert.NotErrorIs(t, err, identity.ErrUserNotFound)
	assert.Contains(t, err.Error(), "user scan")
}

// UpdateProfile must persist all mutable fields and refresh updated_at.
func TestUserRepo_UpdateProfile(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()
	repo := postgres.NewUserRepo(pool)

	emp := "E100"
	plant := "F12B-3F"
	u := &identity.User{
		PrimaryEmail: "profile@example.com",
		DisplayName:  "Before",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
		EmployeeID:   &emp,
		Plant:        &plant,
	}
	require.NoError(t, repo.Create(ctx, u))

	newEmp := "E200"
	newDept := "Logistics"
	u.DisplayName = "After"
	u.Role = identity.RoleVendorOperator
	u.Status = identity.StatusSuspended
	u.EmployeeID = &newEmp
	u.Department = &newDept
	require.NoError(t, repo.UpdateProfile(ctx, u))

	got, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	assert.Equal(t, "After", got.DisplayName)
	assert.Equal(t, identity.RoleVendorOperator, got.Role)
	assert.Equal(t, identity.StatusSuspended, got.Status)
	require.NotNil(t, got.EmployeeID)
	assert.Equal(t, "E200", *got.EmployeeID)
	require.NotNil(t, got.Department)
	assert.Equal(t, "Logistics", *got.Department)
}

// UpdateProfile must surface an error from the RETURNING scan (covers its
// error path) when the pool is closed.
func TestUserRepo_UpdateProfile_Error(t *testing.T) {
	pool := closedPool(t)
	repo := postgres.NewUserRepo(pool)
	err := repo.UpdateProfile(context.Background(), &identity.User{
		ID:           "00000000-0000-0000-0000-000000000000",
		PrimaryEmail: "x@example.com",
		DisplayName:  "X",
		Role:         identity.RoleEmployee,
		Status:       identity.StatusActive,
	})
	require.Error(t, err)
}
