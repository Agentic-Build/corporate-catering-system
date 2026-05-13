package payrollhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/payroll"
)

// API exposes payroll endpoints: admin batch + dispute management plus
// employee dispute submission. Admin routes require welfare_admin; the
// employee dispute endpoints require the employee role.
type API struct {
	Svc *payroll.Service
}

// ----- DTOs -----

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
	EntryID      string  `json:"entry_id"`
	OrderID      string  `json:"order_id"`
	OpenedBy     string  `json:"opened_by"`
	Reason       string  `json:"reason"`
	Status       string  `json:"status"`
	Resolution   string  `json:"resolution"`
	ResolvedBy   *string `json:"resolved_by,omitempty"`
	ResolvedAt   *string `json:"resolved_at,omitempty"`
	RefundMinor  int64   `json:"refund_minor"`
}

func toBatchDTO(b *payroll.Batch) batchDTO {
	d := batchDTO{
		ID:          b.ID,
		PeriodStart: b.PeriodStart.UTC().Format("2006-01-02"),
		PeriodEnd:   b.PeriodEnd.UTC().Format("2006-01-02"),
		Status:      string(b.Status),
	}
	if b.LockedAt != nil {
		s := b.LockedAt.UTC().Format(time.RFC3339)
		d.LockedAt = &s
	}
	if b.LockedBy != nil {
		s := *b.LockedBy
		d.LockedBy = &s
	}
	if b.ExportedAt != nil {
		s := b.ExportedAt.UTC().Format(time.RFC3339)
		d.ExportedAt = &s
	}
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
	if d.ResolvedAt != nil {
		s := d.ResolvedAt.UTC().Format(time.RFC3339)
		out.ResolvedAt = &s
	}
	return out
}

// ----- Inputs / Outputs -----

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

// ----- Registration -----

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
}

// ----- Auth helpers -----

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, huma.Error403Forbidden("admin role required")
	}
	return u, nil
}

func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleEmployee {
		return nil, huma.Error403Forbidden("employee role required")
	}
	return u, nil
}

// parseDay parses YYYY-MM-DD into UTC midnight.
func parseDay(s string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", s, time.UTC)
}

// ----- Handlers -----

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

// mapErr translates payroll sentinels to huma HTTP errors.
func mapErr(err error) error {
	switch {
	case errors.Is(err, payroll.ErrBatchNotFound),
		errors.Is(err, payroll.ErrEntryNotFound),
		errors.Is(err, payroll.ErrDisputeNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, payroll.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, payroll.ErrBatchLocked),
		errors.Is(err, payroll.ErrBatchPeriodExists),
		errors.Is(err, payroll.ErrInvalidTransition):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
