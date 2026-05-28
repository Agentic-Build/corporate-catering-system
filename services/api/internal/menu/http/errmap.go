package mhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
)

var errRules = []httpserver.Rule{
	{Err: menu.ErrItemNotFound, Status: http.StatusNotFound},
	{Err: menu.ErrCategoryNotFound, Status: http.StatusNotFound},
	{Err: menu.ErrImageNotFound, Status: http.StatusNotFound},
	{Err: menu.ErrForbidden, Status: http.StatusForbidden},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
