// Package mcpserver wires the MCP server that lets AI agents call T-Bite
// business operations through the same Service layer the HTTP API uses.
package mcpserver

import (
	"context"
	plaudit "github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/audit"

	"github.com/jackc/pgx/v5"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// Shared tool-error strings (kept in one place so wording matches across tools).
const (
	errNotAuthenticated        = "not authenticated"
	errMenuNotConfigured       = "menu service not configured"
	errOrderNotConfigured      = "order service not configured"
	errPayrollNotConfigured    = "payroll service not configured"
	errVendorNotConfigured     = "vendor service not configured"
	errComplianceNotConfigured = "compliance service not configured"
	errPlantRequired           = "plant required (no home plant on user)"
	errRoleCannotReadMenu      = "role %s cannot read menu"
	dateLayoutISO              = "2006-01-02"
)

// AuditTx is the audit_event write surface mcpserver depends on.
type AuditTxWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e plaudit.Entry) error
}

// txBeginner is the transaction-starting surface of *pgxpool.Pool.
type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Deps carries underlying services so MCP tools share HTTP business rules.
// Pool + Audit are optional: when nil, the per-tool audit row is skipped.
type Deps struct {
	Pool       txBeginner
	Audit      AuditTxWriter
	Order      *order.Service
	Vendor     *vendor.Service
	Menu       *menu.Service
	Payroll    *payroll.Service
	Compliance *compliance.Service
	Feedback   *feedback.Service
	Settlement *settlement.Service
	Users      identity.UserRepository
	Sessions   identity.SessionStore
}

// New constructs the MCP server with all tools registered. Each handler
// parses args, enforces the same role rules as HTTP, delegates to the
// underlying Service, then writes an audit row via auditAfter
// (request_id="mcp:<tool>"). tools/list is the authoritative runtime list.
func New(deps Deps) *server.MCPServer {
	s := server.NewMCPServer(
		"T-Bite MCP",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithHooks(buildMetricsHooks()),
	)
	registerOrderTools(s, deps)
	registerMenuTools(s, deps)
	registerVendorTools(s, deps)
	registerPayrollTools(s, deps)
	registerAuditTools(s, deps)
	registerFeedbackTools(s, deps)
	registerSettlementTools(s, deps)
	registerComplianceTools(s, deps)
	registerChatGPTTools(s, deps)
	return s
}

// userFromCtx returns the user attached by idhttp.AuthMiddleware (stdio mode
// pre-attaches the same way so handlers stay uniform).
func userFromCtx(ctx context.Context) (*identity.User, bool) {
	return idhttp.UserFromContext(ctx)
}

// auditAfter writes an audit_event for the MCP tool invocation
// (action="mcp.<tool>", request_id="mcp:<tool>"). Best-effort: a failed audit
// never fails the tool.
func auditAfter(ctx context.Context, deps Deps, toolName, targetKind, targetID string, payload map[string]any, user *identity.User) {
	if deps.Pool == nil || deps.Audit == nil || user == nil {
		return
	}
	actorID := user.ID
	actorRole := string(user.Role)
	_ = pgx.BeginFunc(ctx, deps.Pool, func(tx pgx.Tx) error {
		return deps.Audit.WriteTx(ctx, tx, plaudit.Entry{ActorID: &actorID, ActorRole: &actorRole, Action: "mcp." + toolName, TargetKind: targetKind, TargetID: targetID, Payload: payload, RequestID: "mcp:" + toolName})
	})
}
