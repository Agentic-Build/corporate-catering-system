package dlqhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/dlq"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
)

var errRules = []httpserver.Rule{
	{Err: dlq.ErrMessageNotFound, Status: http.StatusNotFound},
	{Err: dlq.ErrAlreadyResolved, Status: http.StatusConflict},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
