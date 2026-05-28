package qhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/quota"
)

var errRules = []httpserver.Rule{
	{Err: quota.ErrSupplyNotFound, Status: http.StatusNotFound},
	{Err: menu.ErrItemNotFound, Status: http.StatusNotFound},
	{Err: menu.ErrForbidden, Status: http.StatusForbidden},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
