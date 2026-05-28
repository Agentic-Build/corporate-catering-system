package payrollhttp

import (
	"context"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

type listDisputesInput struct {
	Status string `query:"status" enum:"open,resolved_refund,resolved_reject,cancelled,"`
}

type listDisputesOutput struct {
	Body struct {
		Items []disputeDTO `json:"items"`
	}
}

type resolveDisputeInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Status      string `json:"status" enum:"resolved_refund,resolved_reject"`
		Resolution  string `json:"resolution"`
		RefundMinor int64  `json:"refund_minor"`
	}
}

type openDisputeInput struct {
	Body struct {
		OrderID string `json:"order_id" format:"uuid"`
		Reason  string `json:"reason" minLength:"1"`
	}
}

type disputeOutput struct {
	Body struct {
		Dispute disputeDTO `json:"dispute"`
	}
}

func (a *API) listDisputes(ctx context.Context, in *listDisputesInput) (*listDisputesOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	var statuses []payroll.DisputeStatus
	if in.Status != "" {
		statuses = []payroll.DisputeStatus{payroll.DisputeStatus(in.Status)}
	}
	ds, err := a.Svc.ListDisputes(ctx, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listDisputesOutput
	resp.Body.Items = make([]disputeDTO, 0, len(ds))
	for _, d := range ds {
		resp.Body.Items = append(resp.Body.Items, toDisputeDTO(d))
	}
	return &resp, nil
}

func (a *API) resolveDispute(ctx context.Context, in *resolveDisputeInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.ResolveDispute(ctx, payroll.ResolveDisputeInput{
		DisputeID:   in.ID,
		ResolvedBy:  u.ID,
		Status:      payroll.DisputeStatus(in.Body.Status),
		Resolution:  in.Body.Resolution,
		RefundMinor: in.Body.RefundMinor,
	}); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) openDispute(ctx context.Context, in *openDisputeInput) (*disputeOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	d, err := a.Svc.OpenDisputeByOrder(ctx, in.Body.OrderID, u.ID, in.Body.Reason)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp disputeOutput
	resp.Body.Dispute = toDisputeDTO(d)
	return &resp, nil
}

func (a *API) listMyDisputes(ctx context.Context, _ *struct{}) (*listDisputesOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	ds, err := a.Svc.ListMyDisputes(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listDisputesOutput
	resp.Body.Items = make([]disputeDTO, 0, len(ds))
	for _, d := range ds {
		resp.Body.Items = append(resp.Body.Items, toDisputeDTO(d))
	}
	return &resp, nil
}
