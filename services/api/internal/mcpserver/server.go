// Package mcpserver wires the MCP (Model Context Protocol) server that lets AI
// agents call T-Bite business operations through the same Service layer the
// HTTP API uses. Task 1 ships the skeleton only — tools are registered in
// subsequent P7 tasks.
package mcpserver

import (
	"context"

	"github.com/mark3labs/mcp-go/server"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// Deps carries the underlying services the MCP layer reuses. MCP tools call
// these Services directly so business rules stay identical to the HTTP path.
type Deps struct {
	Order      *order.Service
	Vendor     *vendor.Service
	Payroll    *payroll.Service
	Compliance *compliance.Service
	Users      identity.UserRepository
	Sessions   identity.SessionStore
}

// New constructs the MCP server with all tools registered.
//
// P7 Task 2/3 registers 5 read-only + 3 employee write tools (8 total). Each
// tool handler parses arguments, enforces the same role rules used by HTTP
// handlers, then delegates to the underlying Service so business semantics
// stay identical across transports.
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
