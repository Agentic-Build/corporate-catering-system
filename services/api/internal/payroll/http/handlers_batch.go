package payrollhttp

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

type createBatchInput struct {
	Body struct {
		PeriodStart string `json:"period_start"`
		PeriodEnd   string `json:"period_end"`
	}
}

type batchOutput struct {
	Body struct {
		Batch batchDTO `json:"batch"`
	}
}

type listBatchesInput struct {
	Status string `query:"status" enum:"draft,locked,exported,closed,"`
}

type listBatchesOutput struct {
	Body struct {
		Items []batchDTO `json:"items"`
	}
}

type batchWithEntriesOutput struct {
	Body struct {
		Batch   batchDTO   `json:"batch"`
		Entries []entryDTO `json:"entries"`
	}
}

func (a *API) createBatch(ctx context.Context, in *createBatchInput) (*batchOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	start, err := parseDay(in.Body.PeriodStart)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period_start (YYYY-MM-DD)")
	}
	end, err := parseDay(in.Body.PeriodEnd)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid period_end (YYYY-MM-DD)")
	}
	b, err := a.Svc.BuildDraft(ctx, payroll.BuildDraftInput{PeriodStart: start, PeriodEnd: end})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp batchOutput
	resp.Body.Batch = toBatchDTO(b)
	return &resp, nil
}

func (a *API) listBatches(ctx context.Context, in *listBatchesInput) (*listBatchesOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	var statuses []payroll.BatchStatus
	if in.Status != "" {
		statuses = []payroll.BatchStatus{payroll.BatchStatus(in.Status)}
	}
	bs, err := a.Svc.ListBatches(ctx, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listBatchesOutput
	resp.Body.Items = make([]batchDTO, 0, len(bs))
	for _, b := range bs {
		resp.Body.Items = append(resp.Body.Items, toBatchDTO(b))
	}
	return &resp, nil
}

func (a *API) getBatch(ctx context.Context, in *batchIDInput) (*batchWithEntriesOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	b, err := a.Svc.GetBatch(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	entries, err := a.Svc.ListBatchEntries(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp batchWithEntriesOutput
	resp.Body.Batch = toBatchDTO(b)
	resp.Body.Entries = make([]entryDTO, 0, len(entries))
	for _, e := range entries {
		resp.Body.Entries = append(resp.Body.Entries, toEntryDTO(e))
	}
	return &resp, nil
}

func (a *API) lockBatch(ctx context.Context, in *batchIDInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Lock(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}
