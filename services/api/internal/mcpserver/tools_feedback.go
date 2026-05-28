// Package mcpserver — feedback tools.
//
// Employee-facing write operations over feedback.Service: submit a meal rating
// and file a complaint for a picked-up order. Each handler enforces the
// employee role gate, delegates to the Service so business rules stay
// identical to the HTTP path, then writes an audit_event row tagged with
// request_id="mcp:<tool>".
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

func registerFeedbackTools(s *server.MCPServer, deps Deps) {
	s.AddTool(
		mcp.NewTool("feedback.rate_order",
			mcp.WithDescription("Submit a meal rating for a picked-up order (employee owner only)"),
			mcp.WithString("order_id",
				mcp.Required(),
				mcp.Description("UUID of the picked-up order to rate"),
			),
			mcp.WithNumber("score",
				mcp.Required(),
				mcp.Description("Rating score, integer 1-5"),
			),
			mcp.WithString("comment",
				mcp.Description("Optional free-text comment, up to 500 characters"),
			),
			annoCreate(),
		),
		feedbackRateOrderHandler(deps),
	)
	s.AddTool(
		mcp.NewTool("feedback.file_complaint",
			mcp.WithDescription("File a complaint for a picked-up order (employee owner only)"),
			mcp.WithString("order_id",
				mcp.Required(),
				mcp.Description("UUID of the picked-up order to complain about"),
			),
			mcp.WithString("category",
				mcp.Required(),
				mcp.Description("wrong_item | missing_item | quality | portion | hygiene | other"),
			),
			mcp.WithString("description",
				mcp.Required(),
				mcp.Description("Complaint description, 5-1000 characters"),
			),
			annoCreate(),
		),
		feedbackFileComplaintHandler(deps),
	)
}

// feedbackEmployeePrelude validates auth + employee role + Feedback wired.
func feedbackEmployeePrelude(ctx context.Context, deps Deps, denyMsg string) (*identity.User, *mcp.CallToolResult) {
	u, ok := userFromCtx(ctx)
	if !ok {
		return nil, mcp.NewToolResultError(errNotAuthenticated)
	}
	if u.Role != identity.RoleEmployee {
		return nil, mcp.NewToolResultError(denyMsg)
	}
	if deps.Feedback == nil {
		return nil, mcp.NewToolResultError("feedback service not configured")
	}
	return u, nil
}

func feedbackRateOrderHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := feedbackEmployeePrelude(ctx, deps, "only employee can rate orders")
		if errRes != nil {
			return errRes, nil
		}
		orderID, err := req.RequireString("order_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		score := int(req.GetFloat("score", 0))
		comment := req.GetString("comment", "")
		r, err := deps.Feedback.RateOrder(ctx, feedback.RateOrderInput{
			OrderID: orderID,
			UserID:  u.ID,
			Score:   score,
			Comment: comment,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "feedback.rate_order", "meal_rating", r.ID, map[string]any{
			"order_id": orderID,
			"score":    score,
		}, u)
		data, _ := json.Marshal(r)
		return mcp.NewToolResultText(string(data)), nil
	}
}

func feedbackFileComplaintHandler(deps Deps) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		u, errRes := feedbackEmployeePrelude(ctx, deps, "only employee can file complaints")
		if errRes != nil {
			return errRes, nil
		}
		orderID, err := req.RequireString("order_id")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		category, err := req.RequireString("category")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		description, err := req.RequireString("description")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		c, err := deps.Feedback.FileComplaint(ctx, feedback.FileComplaintInput{
			OrderID:     orderID,
			UserID:      u.ID,
			Category:    feedback.ComplaintCategory(category),
			Description: description,
		})
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		auditAfter(ctx, deps, "feedback.file_complaint", "meal_complaint", c.ID, map[string]any{
			"order_id": orderID,
			"category": category,
		}, u)
		data, _ := json.Marshal(c)
		return mcp.NewToolResultText(string(data)), nil
	}
}
