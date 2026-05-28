package chttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

var errRules = []httpserver.Rule{
	{Err: compliance.ErrDocumentNotFound, Status: http.StatusNotFound},
	{Err: compliance.ErrAnomalyNotFound, Status: http.StatusNotFound},
	{Err: vendor.ErrVendorNotFound, Status: http.StatusNotFound},
	{Err: compliance.ErrInvalidStatus, Status: http.StatusConflict},
	{Err: compliance.ErrInvalidResupply, Status: http.StatusConflict},
	{Err: compliance.ErrInvalidAction, Status: http.StatusBadRequest},
	{Err: compliance.ErrInvalidFilename, Status: http.StatusBadRequest},
	{Err: compliance.ErrFileTooLarge, Status: http.StatusBadRequest},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
