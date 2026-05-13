// Package mcpserver wires the MCP (Model Context Protocol) server that lets AI
// agents call T-Bite business operations through the same Service layer the
// HTTP API uses. Task 1 ships the skeleton only — tools are registered in
// subsequent P7 tasks.
package mcpserver

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// AuditTx is the minimal interface mcpserver depends on for audit_event writes.
// The concrete *order/postgres.AuditRepo (re-used by every other Service)
// satisfies it. Kept tiny so tests can stub without dragging in pgxpool.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// Deps carries the underlying services the MCP layer reuses. MCP tools call
// these Services directly so business rules stay identical to the HTTP path.
//
// Pool + Audit are used by auditAfter to write the per-tool audit_event row
// with request_id="mcp:<tool>". They're optional: when nil, audit is skipped
// (the business operation itself already succeeded).
type Deps struct {
	Pool       *pgxpool.Pool
	Audit      AuditTx
	Order      *order.Service
	Vendor     *vendor.Service
	Payroll    *payroll.Service
	Compliance *compliance.Service
	Users      identity.UserRepository
	Sessions   identity.SessionStore
}

// New constructs the MCP server with all tools registered.
//
// P7 Tasks 2-4 register 12 tools total: 5 read-only + 3 employee write +
// 4 admin write. Each handler parses arguments, enforces the same role rules
// used by HTTP handlers, delegates to the underlying Service, and then writes
// an audit_event row via auditAfter (request_id="mcp:<tool>").
func New(deps Deps) *server.MCPServer {
	s := server.NewMCPServer(
		"T-Bite MCP",
		"0.1.0",
		server.WithToolCapabilities(true),
	)
	registerOrderTools(s, deps)
	registerVendorTools(s, deps)
	registerPayrollTools(s, deps)
	registerAuditTools(s, deps)
	return s
}

// userFromCtx returns the authenticated user attached by idhttp.AuthMiddleware.
// In stdio mode (P7 Task 6) the entrypoint will pre-attach the user the same
// way before invoking tool handlers, so handlers can call this uniformly.
func userFromCtx(ctx context.Context) (*identity.User, bool) {
	return idhttp.UserFromContext(ctx)
}

// auditAfter writes an audit_event row attributing the MCP tool invocation
// to the authenticated user. Failures are best-effort: the business operation
// has already succeeded, so we never fail the tool because of an audit miss.
//
// action     = "mcp.<toolName>"
// request_id = "mcp:<toolName>"  (lets admins filter MCP-originated actions)
func auditAfter(ctx context.Context, deps Deps, toolName, targetKind, targetID string, payload map[string]any, user *identity.User) {
	if deps.Pool == nil || deps.Audit == nil || user == nil {
		return
	}
	actorID := user.ID
	actorRole := string(user.Role)
	_ = pgx.BeginFunc(ctx, deps.Pool, func(tx pgx.Tx) error {
		return deps.Audit.WriteTx(ctx, tx, &actorID, &actorRole,
			"mcp."+toolName, targetKind, targetID, payload, "mcp:"+toolName)
	})
}
