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

// batchIDInput is shared by batch + exception handlers (the listExceptions
// path takes the parent batch id).
type batchIDInput struct {
	ID string `path:"id" format:"uuid"`
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
