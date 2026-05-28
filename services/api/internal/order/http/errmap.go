package ohttp

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
)

var errRules = []httpserver.Rule{
	{Err: order.ErrOrderNotFound, Status: http.StatusNotFound},
	{Err: order.ErrForbidden, Status: http.StatusForbidden},
	{Err: order.ErrInvalidTransition, Status: http.StatusConflict},
	{Err: order.ErrCutoffPassed, Status: http.StatusConflict},
	{Err: order.ErrConcurrentModification, Status: http.StatusConflict},
	{Err: quota.ErrOutOfStock, Status: http.StatusConflict},
	{Err: quota.ErrSupplyNotFound, Status: http.StatusConflict},
	{Err: order.ErrEmptyOrder, Status: http.StatusBadRequest},
	{Err: order.ErrMultiVendor, Status: http.StatusBadRequest},
	{Err: order.ErrVendorPlantMismatch, Status: http.StatusBadRequest},
	{Err: order.ErrPlantMismatch, Status: http.StatusBadRequest},
	{Err: order.ErrOutsidePreorderWindow, Status: http.StatusBadRequest},
}

// mapErr translates domain errors to huma HTTP errors. Unmapped errors are
// logged so race-condition / leaked-tx classes show up in production logs.
func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	slog.Error("order http unmapped error → 500",
		"err", err.Error(),
		"type", fmt.Sprintf("%T", err),
	)
	return huma.Error500InternalServerError("internal", err)
}
