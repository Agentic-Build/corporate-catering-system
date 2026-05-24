package settlement

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

const auditActorRole = "welfare_admin"

// Service orchestrates vendor settlement: admin period close / void plus the
// merchant-facing reconciliation and settlement reads. Close is a multi-row
// write (one vendor_settlement per vendor + audit_event) so it runs inside
// pgx.BeginFunc to keep a half-closed period from surviving a crash.
type Service struct {
	Pool        *pgxpool.Pool
	Settlements SettlementRepository
	Orders      OrderAggregateRepository
	Audit       AuditTx
}

// CloseSettlementInput selects the period to settle.
type CloseSettlementInput struct {
	PeriodStart time.Time
	PeriodEnd   time.Time
	ClosedBy    string
}

// CloseSettlement aggregates every vendor's picked_up/no_show orders in the
// period and writes one vendor_settlement row per vendor that has orders. All
// rows plus the audit_event commit in a single transaction. Re-closing a period
// that already has an active (status='closed') row for any of those vendors is
// rejected with ErrPeriodAlreadyClosed — void the prior settlement first.
func (s *Service) CloseSettlement(ctx context.Context, in CloseSettlementInput) ([]*Settlement, error) {
	startedAt := time.Now()
	if in.PeriodStart.After(in.PeriodEnd) {
		return nil, ErrInvalidPeriod
	}

	aggs, err := s.Orders.AggregateByVendor(ctx, in.PeriodStart, in.PeriodEnd)
	if err != nil {
		return nil, err
	}
	if len(aggs) == 0 {
		return nil, ErrNoOrdersInPeriod
	}

	closedBy := in.ClosedBy
	out := make([]*Settlement, 0, len(aggs))
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		for _, a := range aggs {
			st := &Settlement{
				VendorID:     a.VendorID,
				PeriodStart:  in.PeriodStart,
				PeriodEnd:    in.PeriodEnd,
				OrderCount:   a.OrderCount,
				PortionCount: a.PortionCount,
				GrossMinor:   a.GrossMinor,
				OrderIDs:     a.OrderIDs,
				Status:       StatusClosed,
				ClosedBy:     &closedBy,
			}
			if err := s.Settlements.CreateTx(ctx, tx, st); err != nil {
				return err
			}
			out = append(out, st)
		}
		actorRole := auditActorRole
		payload := map[string]any{
			"period_start": in.PeriodStart.Format("2006-01-02"),
			"period_end":   in.PeriodEnd.Format("2006-01-02"),
			"vendor_count": len(out),
		}
		return s.Audit.WriteTx(ctx, tx, &closedBy, &actorRole, "settlement.close", "vendor_settlement_period", in.PeriodStart.Format("2006-01-02")+"/"+in.PeriodEnd.Format("2006-01-02"), payload, "")
	})
	if err != nil {
		return nil, err
	}
	dur := time.Since(startedAt).Seconds()
	for _, st := range out {
		observability.RecordSettlementRun(ctx, st.VendorID, "closed", dur, st.GrossMinor)
	}
	return out, nil
}

// VoidSettlement flips a closed settlement to void so the period can be
// re-closed after correction. The status flip + audit_event commit together.
func (s *Service) VoidSettlement(ctx context.Context, id, voidedBy string) error {
	st, err := s.Settlements.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if st.Status != StatusClosed {
		return ErrInvalidTransition
	}
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Settlements.VoidTx(ctx, tx, id); err != nil {
			return err
		}
		actorRole := auditActorRole
		payload := map[string]any{
			"settlement_id": id,
			"vendor_id":     st.VendorID,
			"period_start":  st.PeriodStart.Format("2006-01-02"),
			"period_end":    st.PeriodEnd.Format("2006-01-02"),
		}
		return s.Audit.WriteTx(ctx, tx, &voidedBy, &actorRole, "settlement.void", "vendor_settlement", id, payload, "")
	})
}

// Reconciliation computes a vendor's live monthly summary straight from the
// order table — used before a period is closed. gross_minor / portion_count use
// the same picked_up/no_show inclusion as CloseSettlement; the breakdown also
// surfaces cancelled/refunded counts for transparency.
func (s *Service) Reconciliation(ctx context.Context, vendorID string, start, end time.Time) (*Reconciliation, error) {
	if start.After(end) {
		return nil, ErrInvalidPeriod
	}
	agg, err := s.Orders.AggregateForVendor(ctx, vendorID, start, end)
	if err != nil {
		return nil, err
	}
	breakdown, err := s.Orders.StatusBreakdownForVendor(ctx, vendorID, start, end)
	if err != nil {
		return nil, err
	}
	return &Reconciliation{
		VendorID:     vendorID,
		PeriodStart:  start,
		PeriodEnd:    end,
		OrderCount:   agg.OrderCount,
		PortionCount: agg.PortionCount,
		GrossMinor:   agg.GrossMinor,
		Breakdown:    breakdown,
	}, nil
}

// ListVendorSettlements returns a vendor's closed/void settlements, newest first.
func (s *Service) ListVendorSettlements(ctx context.Context, vendorID string) ([]*Settlement, error) {
	return s.Settlements.ListByVendor(ctx, vendorID)
}

// GetVendorSettlement fetches one settlement and verifies it belongs to the
// calling vendor. A mismatch is reported as ErrSettlementNotFound so a merchant
// cannot probe other vendors' settlement ids.
func (s *Service) GetVendorSettlement(ctx context.Context, vendorID, id string) (*Settlement, []*SettlementOrderLine, error) {
	st, err := s.Settlements.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if st.VendorID != vendorID {
		return nil, nil, ErrSettlementNotFound
	}
	lines, err := s.Orders.OrderLinesByIDs(ctx, st.OrderIDs)
	if err != nil {
		return nil, nil, err
	}
	return st, lines, nil
}

// ListSettlementsByPeriod returns every vendor's settlements overlapping the
// period — the admin all-vendor overview.
func (s *Service) ListSettlementsByPeriod(ctx context.Context, start, end time.Time) ([]*Settlement, error) {
	if start.After(end) {
		return nil, ErrInvalidPeriod
	}
	return s.Settlements.ListByPeriod(ctx, start, end)
}
