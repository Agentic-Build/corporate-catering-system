package phttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
)

var errRules = []httpserver.Rule{
	{Err: plants.ErrInvalid, Status: http.StatusBadRequest},
	{Err: plants.ErrPlantNotFound, Status: http.StatusNotFound},
	{Err: plants.ErrDuplicateCode, Status: http.StatusConflict},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
