package vhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

var errRules = []httpserver.Rule{
	{Err: vendor.ErrVendorNotFound, Status: http.StatusNotFound},
	{Err: vendor.ErrOperatorNotFound, Status: http.StatusNotFound},
	{Err: vendor.ErrAlreadyApproved, Status: http.StatusConflict},
	{Err: vendor.ErrInvalidStatus, Status: http.StatusConflict},
	{Err: vendor.ErrInvalidOperator, Status: http.StatusBadRequest},
	{Err: vendor.ErrInvalidSettings, Status: http.StatusBadRequest},
	{Err: vendor.ErrProvisioningSetup, Status: http.StatusBadGateway},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
