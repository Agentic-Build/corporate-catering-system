package payrollhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/httpserver"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/payroll"
)

// API exposes payroll endpoints: admin batch + dispute management plus
// employee dispute submission. Admin routes require welfare_admin; the
// employee dispute endpoints require the employee role.
type API struct {
	Svc *payroll.Service
}

type batchDTO struct {
	ID          string  `json:"id"`
	PeriodStart string  `json:"period_start"`
	PeriodEnd   string  `json:"period_end"`
	Status      string  `json:"status"`
	LockedAt    *string `json:"locked_at,omitempty"`
	LockedBy    *string `json:"locked_by,omitempty"`
	ExportedAt  *string `json:"exported_at,omitempty"`
	ExportURI   *string `json:"export_uri,omitempty"`
}

type entryDTO struct {
	ID            string   `json:"id"`
	BatchID       string   `json:"batch_id"`
	UserID        string   `json:"user_id"`
	OrderIDs      []string `json:"order_ids"`
	AmountMinor   int64    `json:"amount_minor"`
	RefundedMinor int64    `json:"refunded_minor"`
}

type disputeDTO struct {
	ID          string  `json:"id"`
	EntryID     *string `json:"entry_id,omitempty"`
	OrderID     string  `json:"order_id"`
	OpenedBy    string  `json:"opened_by"`
	Reason      string  `json:"reason"`
	Status      string  `json:"status"`
	Resolution  string  `json:"resolution"`
	ResolvedBy  *string `json:"resolved_by,omitempty"`
	ResolvedAt  *string `json:"resolved_at,omitempty"`
	RefundMinor int64   `json:"refund_minor"`
}

func toBatchDTO(b *payroll.Batch) batchDTO {
	d := batchDTO{
		ID:          b.ID,
		PeriodStart: b.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:   b.PeriodEnd.UTC().Format("2006-01-02"),
		Status:      string(b.Status),
	}
	d.LockedAt = httpserver.FormatNullableTimePtr(b.LockedAt)
	if b.LockedBy != nil {
		s := *b.LockedBy
		d.LockedBy = &s
	}
	d.ExportedAt = httpserver.FormatNullableTimePtr(b.ExportedAt)
	if b.ExportURI != nil {
		s := *b.ExportURI
		d.ExportURI = &s
	}
	return d
}

func toEntryDTO(e *payroll.Entry) entryDTO {
	orderIDs := e.OrderIDs
	if orderIDs == nil {
		orderIDs = []string{}
	}
	return entryDTO{
		ID:            e.ID,
		BatchID:       e.BatchID,
		UserID:        e.UserID,
		OrderIDs:      orderIDs,
		AmountMinor:   e.AmountMinor,
		RefundedMinor: e.RefundedMinor,
	}
}

func toDisputeDTO(d *payroll.Dispute) disputeDTO {
	out := disputeDTO{
		ID:          d.ID,
		EntryID:     d.EntryID,
		OrderID:     d.OrderID,
		OpenedBy:    d.OpenedBy,
		Reason:      d.Reason,
		Status:      string(d.Status),
		Resolution:  d.Resolution,
		RefundMinor: d.RefundMinor,
	}
	if d.ResolvedBy != nil {
		s := *d.ResolvedBy
		out.ResolvedBy = &s
	}
	out.ResolvedAt = httpserver.FormatNullableTimePtr(d.ResolvedAt)
	return out
}

type exceptionDTO struct {
	ID         string  `json:"id"`
	BatchID    string  `json:"batch_id"`
	EntryID    string  `json:"entry_id"`
	UserID     string  `json:"user_id"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Detail     string  `json:"detail"`
	Resolution string  `json:"resolution"`
	ResolvedBy *string `json:"resolved_by,omitempty"`
	ResolvedAt *string `json:"resolved_at,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

func toExceptionDTO(e *payroll.Exception) exceptionDTO {
	out := exceptionDTO{
		ID:         e.ID,
		BatchID:    e.BatchID,
		EntryID:    e.EntryID,
		UserID:     e.UserID,
		Kind:       string(e.Kind),
		Status:     string(e.Status),
		Detail:     e.Detail,
		Resolution: e.Resolution,
		CreatedAt:  e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.ResolvedBy != nil {
		s := *e.ResolvedBy
		out.ResolvedBy = &s
	}
	out.ResolvedAt = httpserver.FormatNullableTimePtr(e.ResolvedAt)
	return out
}

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

type batchIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type batchWithEntriesOutput struct {
	Body struct {
		Batch   batchDTO   `json:"batch"`
		Entries []entryDTO `json:"entries"`
	}
}

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

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "createPayrollBatch",
		Method:        http.MethodPost,
		Path:          "/api/admin/payroll/batches",
		Summary:       "Build a draft payroll batch for a period",
		Tags:          []string{"admin", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.createBatch)

	huma.Register(api, huma.Operation{
		OperationID: "listPayrollBatches",
		Method:      http.MethodGet,
		Path:        "/api/admin/payroll/batches",
		Summary:     "List payroll batches",
		Tags:        []string{"admin", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listBatches)

	huma.Register(api, huma.Operation{
		OperationID: "getPayrollBatch",
		Method:      http.MethodGet,
		Path:        "/api/admin/payroll/batches/{id}",
		Summary:     "Get a payroll batch with its entries",
		Tags:        []string{"admin", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getBatch)

	huma.Register(api, huma.Operation{
		OperationID:   "lockPayrollBatch",
		Method:        http.MethodPost,
		Path:          "/api/admin/payroll/batches/{id}/lock",
		Summary:       "Lock a draft batch and emit settlement event",
		Tags:          []string{"admin", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.lockBatch)

	huma.Register(api, huma.Operation{
		OperationID: "listPayrollDisputes",
		Method:      http.MethodGet,
		Path:        "/api/admin/payroll/disputes",
		Summary:     "List payroll disputes (admin)",
		Tags:        []string{"admin", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listDisputes)

	huma.Register(api, huma.Operation{
		OperationID:   "resolvePayrollDispute",
		Method:        http.MethodPost,
		Path:          "/api/admin/payroll/disputes/{id}/resolve",
		Summary:       "Resolve a payroll dispute (refund or reject)",
		Tags:          []string{"admin", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.resolveDispute)

	huma.Register(api, huma.Operation{
		OperationID:   "openMyDispute",
		Method:        http.MethodPost,
		Path:          "/api/employee/disputes",
		Summary:       "Open a dispute against an entry order",
		Tags:          []string{"employee", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.openDispute)

	huma.Register(api, huma.Operation{
		OperationID: "listMyDisputes",
		Method:      http.MethodGet,
		Path:        "/api/employee/disputes",
		Summary:     "List my payroll disputes",
		Tags:        []string{"employee", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listMyDisputes)

	huma.Register(api, huma.Operation{
		OperationID: "listMyPayrollEntries",
		Method:      http.MethodGet,
		Path:        "/api/employee/payroll",
		Summary:     "List my salary-deduction entries across batches",
		Tags:        []string{"employee", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listMyEntries)

	huma.Register(api, huma.Operation{
		OperationID: "getEmployeeCurrentPayroll",
		Method:      http.MethodGet,
		Path:        "/api/employee/payroll/current",
		Summary:     "List my in-progress payroll period's per-order lines",
		Tags:        []string{"employee", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getEmployeeCurrentPayroll)

	huma.Register(api, huma.Operation{
		OperationID: "listPayrollExceptions",
		Method:      http.MethodGet,
		Path:        "/api/admin/payroll/batches/{id}/exceptions",
		Summary:     "List a batch's settlement exceptions (re-runs departed-employee detection)",
		Tags:        []string{"admin", "payroll"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listExceptions)

	huma.Register(api, huma.Operation{
		OperationID:   "flagPayrollException",
		Method:        http.MethodPost,
		Path:          "/api/admin/payroll/batches/{id}/exceptions",
		Summary:       "Flag a batch entry with a manual deduction-failed exception",
		Tags:          []string{"admin", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.flagException)

	huma.Register(api, huma.Operation{
		OperationID:   "resolvePayrollException",
		Method:        http.MethodPost,
		Path:          "/api/admin/payroll/exceptions/{id}/resolve",
		Summary:       "Resolve a settlement exception (resolved or excluded)",
		Tags:          []string{"admin", "payroll"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.resolveException)
}

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireAdmin(ctx)
}

func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireEmployee(ctx)
}

// parseDay parses YYYY-MM-DD into UTC midnight.
func parseDay(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.UTC)
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

