package payrollhttp

import (
	"context"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

type listExceptionsOutput struct {
	Body struct {
		Items []exceptionDTO `json:"items"`
	}
}

type flagExceptionInput struct {
	ID   string `path:"id" format:"uuid" doc:"Payroll batch id"`
	Body struct {
		EntryID string `json:"entry_id" format:"uuid"`
		Detail  string `json:"detail" maxLength:"500"`
	}
}

type resolveExceptionInput struct {
	ID   string `path:"id" format:"uuid" doc:"Payroll exception id"`
	Body struct {
		Status     string `json:"status" enum:"resolved,excluded"`
		Resolution string `json:"resolution" maxLength:"500"`
	}
}

type exceptionOutput struct {
	Body struct {
		Exception exceptionDTO `json:"exception"`
	}
}

func (a *API) listExceptions(ctx context.Context, in *batchIDInput) (*listExceptionsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	exs, err := a.Svc.ListExceptions(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listExceptionsOutput
	resp.Body.Items = make([]exceptionDTO, 0, len(exs))
	for _, e := range exs {
		resp.Body.Items = append(resp.Body.Items, toExceptionDTO(e))
	}
	return &resp, nil
}

func (a *API) flagException(ctx context.Context, in *flagExceptionInput) (*exceptionOutput, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	e, err := a.Svc.FlagException(ctx, payroll.FlagExceptionInput{
		BatchID:   in.ID,
		EntryID:   in.Body.EntryID,
		Detail:    in.Body.Detail,
		FlaggedBy: u.ID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp exceptionOutput
	resp.Body.Exception = toExceptionDTO(e)
	return &resp, nil
}

func (a *API) resolveException(ctx context.Context, in *resolveExceptionInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.ResolveException(ctx, in.ID, payroll.ExceptionStatus(in.Body.Status), in.Body.Resolution, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}
