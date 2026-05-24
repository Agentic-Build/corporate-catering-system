package payroll

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/takalawang/corporate-catering-system/services/api/internal/order"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

// ReverseOrder reverses (沖銷) the salary deduction for a single charged order.
//
// It is the auto-reversal hook for design A3/B3: when a charged order is
// cancelled/refunded the corresponding payroll deduction must be unwound. The
// reversal:
//
//  1. Transitions the order picked_up/no_show → refunded. The conditional
//     UPDATE (WHERE status = from) is what makes the whole operation
//     idempotent — see below.
//  2. If the order is already aggregated into a payroll entry (its batch was
//     locked / exported), bumps that entry's refunded_minor by the order's
//     amount so the HR deduction net (amount − refunded) reflects the reversal.
//     If the order is NOT yet in any entry — it falls in the in-progress,
//     not-yet-locked period — there is nothing to decrement: the refunded
//     status alone surfaces the per-order line as "reversed" in the
//     current-lines view (see current_lines.go), and BuildDraft only
//     aggregates picked_up/no_show orders so a refunded order is never
//     charged into a future batch.
//  3. Emits payroll.order_reversed.v1 + an audit row in the same transaction.
//
// Idempotency: a charged order can be reversed at most once. A replayed call
// finds the order already in `refunded` state and returns nil before opening a
// transaction, so refunded_minor never double-counts and no duplicate
// outbox/audit rows are written.
//
// Only picked_up / no_show orders are reversible — a placed order was never
// charged, so reversing it returns ErrInvalidTransition.
//
// DEFERRED — complaint-resolution-driven reversal: design A3 also calls for a
// reversal when a meal complaint is "resolved with compensation". The current
// feedback/complaint module (internal/feedback) has no compensation outcome:
// complaint resolution is free text only, with no compensation flag, no
// amount, and no order-status change. Wiring money movement off complaint
// resolution would require inventing those semantics, so it is intentionally
// NOT implemented here. Once the complaint module gains an explicit
// "compensate" resolution that carries an amount, that handler can call
// ReverseOrder for a full reversal (or a future amount-aware variant for a
// partial one).
func (s *Service) ReverseOrder(ctx context.Context, orderID string) error {
	o, err := s.Orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	// Already reversed — idempotent no-op. Returning before BeginFunc is what
	// guarantees a replayed event cannot double-count refunded_minor or emit a
	// second outbox/audit row.
	if o.Status == order.StatusRefunded {
		return nil
	}
	// Only a charged order carries a deduction to reverse.
	if o.Status != order.StatusPickedUp && o.Status != order.StatusNoShow {
		return ErrInvalidTransition
	}

	// Locate the payroll entry that aggregated this order, if any. A missing
	// entry (ErrEntryNotFound) is expected for current-period orders.
	entryID, err := s.Entries.FindByOrderForUser(ctx, o.UserID, orderID)
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return err
	}
	hasEntry := err == nil

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.OrderTx.UpdateStatusTx(ctx, tx, orderID, o.Status, order.StatusRefunded); err != nil {
			return err
		}
		if hasEntry {
			if err := s.Entries.IncrementRefundedTx(ctx, tx, entryID, o.TotalPriceMinor); err != nil {
				return err
			}
		}
		sysRole := "welfare_admin"
		payload := map[string]any{
			"order_id":     orderID,
			"user_id":      o.UserID,
			"entry_id":     entryID,
			"refund_minor": o.TotalPriceMinor,
		}
		if err := s.Outbox.AppendTx(ctx, tx, "order", orderID, "payroll.order_reversed.v1", payload, map[string]any{}); err != nil {
			return err
		}
		return s.Audit.WriteTx(ctx, tx, nil, &sysRole, "payroll.order_reverse", "order", orderID, payload, "")
	})
	if err != nil {
		return err
	}
	reason := "auto_refund"
	if !hasEntry {
		reason = "current_period_refund"
	}
	observability.RecordPayrollReversal(ctx, reason)
	return nil
}
