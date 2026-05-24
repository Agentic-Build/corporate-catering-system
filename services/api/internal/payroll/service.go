package payroll

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

// AuditTx mirrors the audit-repo shape used by order.Service so we can share
// the same postgres implementation across services.
type AuditTx interface {
	WriteTx(ctx context.Context, tx pgx.Tx, actorID, actorRole *string, action, targetKind, targetID string, payload map[string]any, requestID string) error
}

// OutboxTx mirrors the outbox-repo shape used by order.Service so payroll
// transitions can append events inside the same transaction.
type OutboxTx interface {
	AppendTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateID, subject string, payload map[string]any, headers map[string]any) error
}

// OrderTx is the order repo subset Service needs inside a transaction
// (specifically: refund transitions picked_up/no_show → refunded).
type OrderTx interface {
	UpdateStatusTx(ctx context.Context, tx pgx.Tx, id string, from, to order.Status) error
}

// OrderRepo is the order repo subset Service needs outside a transaction:
// loading the order for a dispute and listing orders to aggregate into a batch.
type OrderRepo interface {
	GetByID(ctx context.Context, id string) (*order.Order, error)
	ListPickedOrNoShowInPeriod(ctx context.Context, from, to time.Time) ([]*order.Order, error)
}

// Clock lets tests pin "now".
type Clock interface{ Now() time.Time }

// Service orchestrates payroll Build / Lock / OpenDispute / ResolveDispute
// across batch / entry / dispute repos plus audit + outbox. All multi-row
// writes happen inside pgx.BeginFunc so partial failure rolls back atomically.
type Service struct {
	Pool         *pgxpool.Pool
	Batches      BatchRepository
	Entries      EntryRepository
	Disputes     DisputeRepository
	Exceptions   ExceptionRepository
	CurrentLines CurrentLinesRepository
	Orders       OrderRepo
	OrderTx      OrderTx
	Audit        AuditTx
	Outbox       OutboxTx
	Clock        Clock
}

// BuildDraftInput selects which supply dates roll into the draft batch.
type BuildDraftInput struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
}

// BuildDraft aggregates every picked_up / no_show order in [PeriodStart,
// PeriodEnd] into per-user entries and inserts them under a fresh draft batch.
// The batch + all entries commit in a single transaction so a half-built batch
// cannot survive a crash.
func (s *Service) BuildDraft(ctx context.Context, in BuildDraftInput) (*Batch, error) {
	if in.PeriodStart.After(in.PeriodEnd) {
		return nil, fmt.Errorf("payroll: period_start must be <= period_end")
	}

	// Reject duplicate periods up front so the unique-index error surfaces as
	// a typed sentinel instead of a generic pg error.
	existing, err := s.Batches.GetByPeriod(ctx, in.PeriodStart, in.PeriodEnd)
	if err != nil && !errors.Is(err, ErrBatchNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, ErrBatchPeriodExists
	}

	orders, err := s.Orders.ListPickedOrNoShowInPeriod(ctx, in.PeriodStart, in.PeriodEnd)
	if err != nil {
		return nil, err
	}

	type acc struct {
		orderIDs []string
		amount   int64
	}
	byUser := map[string]*acc{}
	for _, o := range orders {
		a, ok := byUser[o.UserID]
		if !ok {
			a = &acc{}
			byUser[o.UserID] = a
		}
		a.orderIDs = append(a.orderIDs, o.ID)
		a.amount += o.TotalPriceMinor
	}

	batch := &Batch{
		PeriodStart: in.PeriodStart,
		PeriodEnd:   in.PeriodEnd,
		Status:      BatchStatusDraft,
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Batches.CreateTx(ctx, tx, batch); err != nil {
			return err
		}
		for userID, a := range byUser {
			entry := &Entry{
				BatchID:     batch.ID,
				UserID:      userID,
				OrderIDs:    a.orderIDs,
				AmountMinor: a.amount,
			}
			if err := s.Entries.CreateTx(ctx, tx, entry); err != nil {
				return err
			}
		}
		// Flag entries whose employee is no longer active so the welfare
		// admin sees the exception list straight away.
		if s.Exceptions != nil {
			if err := s.Exceptions.UpsertDepartedTx(ctx, tx, batch.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	period := batch.PeriodStart.Format("2006-01")
	for _, a := range byUser {
		observability.RecordPayrollEntry(ctx, period, a.amount)
	}
	return batch, nil
}

// Lock transitions a draft batch to locked, emits payroll.batch_locked.v1 for
// the settler worker, and writes an audit record — all in one transaction.
func (s *Service) Lock(ctx context.Context, batchID, adminUserID string) error {
	b, err := s.Batches.GetByID(ctx, batchID)
	if err != nil {
		return err
	}
	if b.Status != BatchStatusDraft {
		return ErrBatchLocked
	}

	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Batches.UpdateStatusTx(ctx, tx, batchID, BatchStatusDraft, BatchStatusLocked, &adminUserID); err != nil {
			return err
		}
		adminRole := "welfare_admin"
		payload := map[string]any{
			"batch_id":     batchID,
			"period_start": b.PeriodStart.Format("2006-01-02"),
			"period_end":   b.PeriodEnd.Format("2006-01-02"),
		}
		if err := s.Outbox.AppendTx(ctx, tx, "payroll_batch", batchID, "payroll.batch_locked.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.Audit.WriteTx(ctx, tx, &adminUserID, &adminRole, "payroll.lock", "payroll_batch", batchID, payload, "")
	})
}

// OpenDisputeInput records who is disputing which order under which entry.
type OpenDisputeInput struct {
	EntryID  string
	OrderID  string
	OpenedBy string
	Reason   string
}

// OpenDispute creates a new open dispute after verifying the requester owns
// the entry and the referenced order actually belongs to that entry's batch.
func (s *Service) OpenDispute(ctx context.Context, in OpenDisputeInput) (*Dispute, error) {
	entry, err := s.Entries.GetByID(ctx, in.EntryID)
	if err != nil {
		return nil, err
	}
	if entry.UserID != in.OpenedBy {
		return nil, ErrForbidden
	}
	matched := false
	for _, id := range entry.OrderIDs {
		if id == in.OrderID {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("payroll: order %s is not part of entry %s", in.OrderID, in.EntryID)
	}
	d := &Dispute{
		EntryID:  in.EntryID,
		OrderID:  in.OrderID,
		OpenedBy: in.OpenedBy,
		Reason:   in.Reason,
		Status:   DisputeStatusOpen,
	}
	if err := s.Disputes.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// OpenDisputeByOrder is the employee-friendly entry point: callers don't need
// to know the entry_id — we look it up from the order. This keeps the public
// API surface (POST /api/employee/disputes) minimal at the cost of one extra
// indexable query per submission.
func (s *Service) OpenDisputeByOrder(ctx context.Context, orderID, openedBy, reason string) (*Dispute, error) {
	entryID, err := s.Entries.FindByOrderForUser(ctx, openedBy, orderID)
	if err != nil {
		return nil, err
	}
	return s.OpenDispute(ctx, OpenDisputeInput{
		EntryID:  entryID,
		OrderID:  orderID,
		OpenedBy: openedBy,
		Reason:   reason,
	})
}

// ResolveDisputeInput captures the resolution path: refund or reject.
type ResolveDisputeInput struct {
	DisputeID   string
	ResolvedBy  string
	Status      DisputeStatus // resolved_refund | resolved_reject
	Resolution  string
	RefundMinor int64
}

// ResolveDispute atomically resolves an open dispute. For resolved_refund it
// also bumps the entry's refunded_minor and transitions the disputed order to
// refunded (if it's still in picked_up/no_show). Same transaction means partial
// failure rolls back the entire resolution.
func (s *Service) ResolveDispute(ctx context.Context, in ResolveDisputeInput) error {
	if in.Status != DisputeStatusResolvedRefund && in.Status != DisputeStatusResolvedReject {
		return fmt.Errorf("payroll: invalid resolution status %q", in.Status)
	}
	d, err := s.Disputes.GetByID(ctx, in.DisputeID)
	if err != nil {
		return err
	}
	if d.Status != DisputeStatusOpen {
		return ErrInvalidTransition
	}

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Disputes.UpdateStatusTx(ctx, tx, in.DisputeID, in.Status, &in.ResolvedBy, in.Resolution, in.RefundMinor); err != nil {
			return err
		}
		if in.Status == DisputeStatusResolvedRefund {
			if in.RefundMinor < 0 {
				return fmt.Errorf("payroll: refund_minor must be >= 0")
			}
			if err := s.Entries.IncrementRefundedTx(ctx, tx, d.EntryID, in.RefundMinor); err != nil {
				return err
			}
			o, err := s.Orders.GetByID(ctx, d.OrderID)
			if err != nil {
				return err
			}
			if o.Status == order.StatusPickedUp || o.Status == order.StatusNoShow {
				if err := s.OrderTx.UpdateStatusTx(ctx, tx, d.OrderID, o.Status, order.StatusRefunded); err != nil {
					return err
				}
			}
		}
		adminRole := "welfare_admin"
		payload := map[string]any{
			"dispute_id":   in.DisputeID,
			"order_id":     d.OrderID,
			"status":       string(in.Status),
			"refund_minor": in.RefundMinor,
		}
		if err := s.Outbox.AppendTx(ctx, tx, "payroll_dispute", in.DisputeID, "payroll.dispute_resolved.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.Audit.WriteTx(ctx, tx, &in.ResolvedBy, &adminRole, "payroll.dispute_resolve", "payroll_dispute", in.DisputeID, payload, "")
	})
	if err != nil {
		return err
	}
	observability.RecordPayrollDispute(ctx, string(in.Status))
	return nil
}

// ListBatches returns batches filtered by status (nil → all).
func (s *Service) ListBatches(ctx context.Context, statuses []BatchStatus) ([]*Batch, error) {
	return s.Batches.List(ctx, statuses)
}

// GetBatch fetches a single batch by id.
func (s *Service) GetBatch(ctx context.Context, id string) (*Batch, error) {
	return s.Batches.GetByID(ctx, id)
}

// ListBatchEntries returns the entries that belong to a batch.
func (s *Service) ListBatchEntries(ctx context.Context, batchID string) ([]*Entry, error) {
	return s.Entries.ListByBatch(ctx, batchID)
}

// ListDisputes returns disputes filtered by status (nil → all). Admin view.
func (s *Service) ListDisputes(ctx context.Context, statuses []DisputeStatus) ([]*Dispute, error) {
	return s.Disputes.ListByStatus(ctx, statuses)
}

// ListMyDisputes returns the disputes a user opened. Employee view.
func (s *Service) ListMyDisputes(ctx context.Context, userID string) ([]*Dispute, error) {
	return s.Disputes.ListByUser(ctx, userID)
}

// ListMyEntries returns the employee's salary-deduction entries across all
// batches, newest period first.
func (s *Service) ListMyEntries(ctx context.Context, userID string) ([]*EmployeeEntry, error) {
	return s.Entries.ListByUser(ctx, userID)
}

// ListCurrentLines returns the per-order lines for the employee's in-progress
// (not-yet-locked) payroll period.
//
// "Current period" is defined as every chargeable order of the employee whose
// supply_date falls *after* the latest locked batch's period_end (or all such
// orders when no locked batch exists yet). We deliberately do NOT key off a
// draft batch row: draft batches are built ad-hoc by a welfare admin via
// BuildDraft, so until an admin acts there is no draft to read — yet the
// employee still has accumulating deductions to show. Anchoring to the last
// *locked* boundary keeps the view correct regardless of whether a draft has
// been built, and never double-counts orders already locked into HR export.
//
// When CurrentLines is left unset, the query runs directly against Pool —
// keeping the endpoint functional without extra wiring.
func (s *Service) ListCurrentLines(ctx context.Context, userID string) ([]CurrentPayrollLine, error) {
	if s.CurrentLines != nil {
		return s.CurrentLines.ListCurrentLines(ctx, userID)
	}
	return QueryCurrentLines(ctx, s.Pool, userID)
}

// FlagExceptionInput records a manual deduction-failed exception.
type FlagExceptionInput struct {
	BatchID   string
	EntryID   string
	Detail    string
	FlaggedBy string
}

// ListExceptions re-runs departed-employee detection (idempotent) and returns
// every exception on the batch. The batch must exist.
func (s *Service) ListExceptions(ctx context.Context, batchID string) ([]*Exception, error) {
	if _, err := s.Batches.GetByID(ctx, batchID); err != nil {
		return nil, err
	}
	if err := s.Exceptions.UpsertDeparted(ctx, batchID); err != nil {
		return nil, err
	}
	return s.Exceptions.ListByBatch(ctx, batchID)
}

// FlagException records a manual deduction_failed exception against a batch
// entry. The entry must belong to the batch.
func (s *Service) FlagException(ctx context.Context, in FlagExceptionInput) (*Exception, error) {
	entry, err := s.Entries.GetByID(ctx, in.EntryID)
	if err != nil {
		return nil, err
	}
	if entry.BatchID != in.BatchID {
		return nil, ErrInvalidException
	}
	e := &Exception{
		BatchID: in.BatchID,
		EntryID: in.EntryID,
		UserID:  entry.UserID,
		Kind:    ExceptionDeductionFailed,
		Status:  ExceptionOpen,
		Detail:  in.Detail,
	}
	if err := s.Exceptions.Create(ctx, e); err != nil {
		return nil, err
	}
	role := "welfare_admin"
	auditErr := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return s.Audit.WriteTx(ctx, tx, &in.FlaggedBy, &role, "payroll.exception_flag", "payroll_exception", e.ID,
			map[string]any{"batch_id": in.BatchID, "entry_id": in.EntryID, "kind": string(ExceptionDeductionFailed)}, "")
	})
	if auditErr != nil {
		return nil, auditErr
	}
	return e, nil
}

// ResolveException marks an exception resolved (handled, still deducted) or
// excluded (dropped from the HR deduction file).
func (s *Service) ResolveException(ctx context.Context, id string, status ExceptionStatus, resolution, resolvedBy string) error {
	if status != ExceptionResolved && status != ExceptionExcluded {
		return ErrInvalidException
	}
	ex, err := s.Exceptions.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if err := s.Exceptions.Resolve(ctx, id, status, resolution, resolvedBy); err != nil {
		return err
	}
	role := "welfare_admin"
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return s.Audit.WriteTx(ctx, tx, &resolvedBy, &role, "payroll.exception_resolve", "payroll_exception", id,
			map[string]any{"batch_id": ex.BatchID, "status": string(status)}, "")
	})
}
