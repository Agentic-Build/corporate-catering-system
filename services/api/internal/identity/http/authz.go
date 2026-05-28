package idhttp

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

// Role gates shared by every bounded context's HTTP layer. These are the single
// source of truth for the "authenticate + check role" pattern; per-context
// handlers delegate to them (some adapt the return shape) so the 401/403
// behaviour stays identical everywhere.

// RequireRole returns the authenticated user iff their role matches, else a
// huma 401 (unauthenticated) or 403 (wrong role) error.
func RequireRole(ctx context.Context, role identity.Role, label string) (*identity.User, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != role {
		return nil, huma.Error403Forbidden(label + " role required")
	}
	return u, nil
}

// RequireEmployee gates an employee-only endpoint.
func RequireEmployee(ctx context.Context) (*identity.User, error) {
	return RequireRole(ctx, identity.RoleEmployee, "employee")
}

// RequireAdmin gates a welfare_admin-only endpoint.
func RequireAdmin(ctx context.Context) (*identity.User, error) {
	return RequireRole(ctx, identity.RoleWelfareAdmin, "welfare_admin")
}

// RequireVendor gates a vendor-operator endpoint and additionally enforces the
// user is bound to a vendor, returning that vendor id.
func RequireVendor(ctx context.Context) (*identity.User, string, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return nil, "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return nil, "", huma.Error403Forbidden("user not bound to a vendor")
	}
	return u, *u.VendorID, nil
}
