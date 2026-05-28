package payrollhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

var errRules = []httpserver.Rule{
	{Err: payroll.ErrBatchNotFound, Status: http.StatusNotFound},
	{Err: payroll.ErrEntryNotFound, Status: http.StatusNotFound},
	{Err: payroll.ErrDisputeNotFound, Status: http.StatusNotFound},
	{Err: payroll.ErrExceptionNotFound, Status: http.StatusNotFound},
	{Err: order.ErrOrderNotFound, Status: http.StatusNotFound},
	{Err: payroll.ErrForbidden, Status: http.StatusForbidden},
	{Err: payroll.ErrInvalidException, Status: http.StatusBadRequest},
	{Err: payroll.ErrRefundExceedsOrder, Status: http.StatusBadRequest},
	{Err: payroll.ErrBatchLocked, Status: http.StatusConflict},
	{Err: payroll.ErrBatchPeriodExists, Status: http.StatusConflict},
	{Err: payroll.ErrInvalidTransition, Status: http.StatusConflict},
	{Err: payroll.ErrOrderNotDisputable, Status: http.StatusConflict},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
