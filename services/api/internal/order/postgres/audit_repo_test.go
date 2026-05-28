package postgres_test

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgrepo "github.com/Agentic-Build/corporate-catering-system/services/api/internal/order/postgres"
)

func TestAuditRepo_WriteInsert(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	uid := seedUser(t, pool, "welfare_admin")
	role := "welfare_admin"
	repo := pgrepo.NewAuditRepo(pool)

	err := repo.Write(ctx, plaudit.Entry{ActorID: &uid, ActorRole: &role, Action: "order.place", TargetKind: "order", TargetID: "00000000-0000-0000-0000-000000000001", Payload: map[string]any{"total": 24000}, RequestID: "req-abc"})
	require.NoError(t, err)

	var count int
	err = pool.QueryRow(ctx, `
SELECT count(*) FROM audit_event
 WHERE action='order.place' AND target_id='00000000-0000-0000-0000-000000000001' AND request_id='req-abc'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestAuditRepo_AppendOnly_UpdateFails(t *testing.T) {
	pool, cleanup := setupPostgres(t)
	defer cleanup()
	ctx := context.Background()

	repo := pgrepo.NewAuditRepo(pool)
	require.NoError(t, repo.Write(ctx, plaudit.Entry{ActorID: nil, ActorRole: nil, Action: "system.boot", TargetKind: "system", TargetID: "tbite", Payload: map[string]any{"v": 1}, RequestID: "req-1"}))

	_, err := pool.Exec(ctx, `UPDATE audit_event SET action='tampered' WHERE action='system.boot'`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")

	_, err = pool.Exec(ctx, `DELETE FROM audit_event WHERE action='system.boot'`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append-only")
}
