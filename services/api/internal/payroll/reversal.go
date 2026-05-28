package payroll

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/order"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/observability"
)

// ReverseOrder reverses (沖銷) the salary deduction for a single charged order.
// Transitions picked_up/no_show → refunded (conditional UPDATE), bumps any
// existing payroll entry's refunded_minor, and emits an outbox + audit row in
// one transaction. Current-period orders (no entry yet) need no decrement —
// the refunded status alone surfaces the line as reversed in current-lines,
// and BuildDraft only aggregates picked_up/no_show into future batches.
//
// Idempotent: a replayed call sees `refunded` and returns before BeginFunc,
// so refunded_minor never double-counts and no duplicate events are emitted.
// Only picked_up/no_show is reversible; placed → ErrInvalidTransition.
func (s *Service) ReverseOrder(ctx context.Context, orderID string) error {
	o, err := s.Orders.GetByID(ctx, orderID)
	if err != nil {
		return err
	}
	// Idempotent no-op: returning before BeginFunc prevents double-count.
	if o.Status == order.StatusRefunded {
		return nil
	}
	if o.Status != order.StatusPickedUp && o.Status != order.StatusNoShow {
		return ErrInvalidTransition
	}

	// Missing entry (ErrEntryNotFound) is expected for current-period orders.
	entryID, err := s.Entries.FindByOrderForUser(ctx, o.UserID, orderID)
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return err
	}
	hasEntry := err == nil

	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		return s.applyReverseOrderTx(ctx, tx, o, orderID, entryID, hasEntry)
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

// applyReverseOrderTx writes the status flip + entry refund-bump + outbox +
// audit row inside the caller's tx.
func (s *Service) applyReverseOrderTx(ctx context.Context, tx pgx.Tx, o *order.Order, orderID, entryID string, hasEntry bool) error {
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
}
