package settlementhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/settlement"
)

var errRules = []httpserver.Rule{
	{Err: settlement.ErrSettlementNotFound, Status: http.StatusNotFound},
	{Err: settlement.ErrForbidden, Status: http.StatusForbidden},
	{Err: settlement.ErrInvalidPeriod, Status: http.StatusBadRequest},
	{Err: settlement.ErrNoOrdersInPeriod, Status: http.StatusBadRequest},
	{Err: settlement.ErrPeriodAlreadyClosed, Status: http.StatusConflict},
	{Err: settlement.ErrInvalidTransition, Status: http.StatusConflict},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
