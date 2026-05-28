package payrollhttp

import "context"

type employeeEntryDTO struct {
	EntryID       string `json:"entry_id"`
	BatchID       string `json:"batch_id"`
	PeriodStart   string `json:"period_start"`
	PeriodEnd     string `json:"period_end"`
	BatchStatus   string `json:"batch_status"`
	OrderCount    int    `json:"order_count"`
	AmountMinor   int64  `json:"amount_minor"`
	RefundedMinor int64  `json:"refunded_minor"`
	NetMinor      int64  `json:"net_minor"`
}

type listMyEntriesOutput struct {
	Body struct {
		Items []employeeEntryDTO `json:"items"`
	}
}

type currentPayrollLineDTO struct {
	OrderID      string  `json:"order_id"`
	SupplyDate   string  `json:"supply_date"`
	VendorName   string  `json:"vendor_name"`
	ItemsSummary string  `json:"items_summary"`
	AmountMinor  int64   `json:"amount_minor"`
	Status       string  `json:"status"`
	Rated        bool    `json:"rated"`
	ComplaintID  *string `json:"complaint_id,omitempty"`
}

type currentPayrollOutput struct {
	Body struct {
		Lines      []currentPayrollLineDTO `json:"lines"`
		TotalMinor int64                   `json:"total_minor"`
	}
}

func (a *API) listMyEntries(ctx context.Context, _ *struct{}) (*listMyEntriesOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := a.Svc.ListMyEntries(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listMyEntriesOutput
	resp.Body.Items = make([]employeeEntryDTO, 0, len(entries))
	for _, e := range entries {
		resp.Body.Items = append(resp.Body.Items, employeeEntryDTO{
			EntryID:       e.EntryID,
			BatchID:       e.BatchID,
			PeriodStart:   e.PeriodStart.UTC().Format("2006-01-02"),
			PeriodEnd:     e.PeriodEnd.UTC().Format("2006-01-02"),
			BatchStatus:   string(e.BatchStatus),
			OrderCount:    e.OrderCount,
			AmountMinor:   e.AmountMinor,
			RefundedMinor: e.RefundedMinor,
			NetMinor:      e.AmountMinor - e.RefundedMinor,
		})
	}
	return &resp, nil
}

// getEmployeeCurrentPayroll returns the calling employee's in-progress
// (not-yet-locked) payroll period as per-order lines. total_minor sums only
// the charged lines — no_show / reversed lines are excluded from the running
// deduction total.
func (a *API) getEmployeeCurrentPayroll(ctx context.Context, _ *struct{}) (*currentPayrollOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	lines, err := a.Svc.ListCurrentLines(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp currentPayrollOutput
	resp.Body.Lines = make([]currentPayrollLineDTO, 0, len(lines))
	for _, l := range lines {
		resp.Body.Lines = append(resp.Body.Lines, currentPayrollLineDTO{
			OrderID:      l.OrderID,
			SupplyDate:   l.SupplyDate,
			VendorName:   l.VendorName,
			ItemsSummary: l.ItemsSummary,
			AmountMinor:  l.AmountMinor,
			Status:       l.Status,
			Rated:        l.Rated,
			ComplaintID:  l.ComplaintID,
		})
		if l.Status == "charged" {
			resp.Body.TotalMinor += l.AmountMinor
		}
	}
	return &resp, nil
}
