package feedbackhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/feedback"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
)

var errRules = []httpserver.Rule{
	{Err: feedback.ErrOrderNotFound, Status: http.StatusNotFound},
	{Err: feedback.ErrComplaintNotFound, Status: http.StatusNotFound},
	{Err: feedback.ErrRatingNotFound, Status: http.StatusNotFound},
	{Err: feedback.ErrForbidden, Status: http.StatusForbidden},
	{Err: feedback.ErrValidation, Status: http.StatusUnprocessableEntity},
	{Err: feedback.ErrOrderNotPickedUp, Status: http.StatusConflict},
	{Err: feedback.ErrAlreadyRated, Status: http.StatusConflict},
	{Err: feedback.ErrComplaintExists, Status: http.StatusConflict},
	{Err: feedback.ErrInvalidTransition, Status: http.StatusConflict},
	{Err: feedback.ErrEscalateTooEarly, Status: http.StatusConflict},
}

func mapErr(err error) error {
	if e := httpserver.Map(err, errRules); e != nil {
		return e
	}
	return huma.Error500InternalServerError("internal", err)
}
