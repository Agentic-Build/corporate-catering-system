// Package scanner contains periodic compliance jobs that run in the
// --role=scheduler binary. DocumentExpiryScanner sweeps approved vendor
// documents and emits anomalies (and flips status=expired for those past
// their expiry date).
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/observability"
)

// DocumentExpiryScanner runs every Interval and:
//  1. Marks approved documents whose expires_at has passed as 'expired' and
//     opens a critical anomaly with kind="document_expired".
//  2. Opens anomalies of kind="document_expiring" for approved documents
//     whose expires_at falls within the next DaysWindow days; severity is
//     derived from days_until_expiry (≤1 critical, ≤7 high, else medium).
//
// All anomaly Open calls are idempotent via the partial unique index on
// (kind, target_kind, target_id) WHERE status='open', so repeated scans
// just refresh payload + severity rather than creating duplicates.
type DocumentExpiryScanner struct {
	Pool       *pgxpool.Pool
	Docs       compliance.DocumentRepository
	Anomaly    compliance.AnomalyRepository
	Interval   time.Duration
	DaysWindow int
	Logger     *slog.Logger
	Clock      clock.Clock
}

// RunOnce executes a single scan pass. Returns the number of documents
// for which an anomaly was successfully opened or status was flipped.
func (s *DocumentExpiryScanner) RunOnce(ctx context.Context) (int, error) {
	if s.DaysWindow <= 0 {
		s.DaysWindow = 14
	}
	now := s.Clock.Now().UTC()
	cutoff := now.AddDate(0, 0, s.DaysWindow)

	pastExpiry, err := s.Docs.ListPastExpiry(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("list past expiry: %w", err)
	}
	expiringSoon, err := s.Docs.ListExpiringBefore(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("list expiring: %w", err)
	}

	handled := 0
	// Past expiry first: flip status + open critical anomaly.
	for _, d := range pastExpiry {
		if err := s.Docs.UpdateStatus(ctx, d.ID, compliance.DocStatusExpired, nil, "auto-expired by scanner"); err != nil {
			s.Logger.Warn("mark expired", "doc_id", d.ID, "err", err)
			continue
		}
		payload := map[string]any{
			"vendor_id": d.VendorID,
			"kind":      string(d.Kind),
			"filename":  d.Filename,
		}
		if d.ExpiresAt != nil {
			payload["expired_at"] = d.ExpiresAt.Format("2006-01-02")
		}
		a := &compliance.Anomaly{
			Kind:        "document_expired",
			TargetKind:  "vendor_document",
			TargetID:    d.ID,
			Severity:    compliance.SeverityCritical,
			Payload:     payload,
			EvidenceURI: []string{d.BlobURI},
		}
		if err := s.Anomaly.Open(ctx, a); err != nil {
			s.Logger.Warn("open expired anomaly", "doc_id", d.ID, "err", err)
			continue
		}
		observability.RecordComplianceViolation(ctx, "document_expired", string(compliance.SeverityCritical), d.VendorID)
		handled++
	}

	// Expiring soon: severity bucketed by days_until_expiry.
	for _, d := range expiringSoon {
		if d.ExpiresAt == nil {
			continue
		}
		// expires_at <= cutoff also includes past-expiry rows. Those were
		// handled above (and just had their status flipped), so skip them
		// here to avoid double-counting.
		if !d.ExpiresAt.After(now) {
			continue
		}
		daysUntil := int(d.ExpiresAt.Sub(now).Hours() / 24)
		var sev compliance.AnomalySeverity
		switch {
		case daysUntil <= 1:
			sev = compliance.SeverityCritical
		case daysUntil <= 7:
			sev = compliance.SeverityHigh
		default:
			sev = compliance.SeverityMedium
		}
		a := &compliance.Anomaly{
			Kind:       "document_expiring",
			TargetKind: "vendor_document",
			TargetID:   d.ID,
			Severity:   sev,
			Payload: map[string]any{
				"vendor_id":         d.VendorID,
				"kind":              string(d.Kind),
				"filename":          d.Filename,
				"days_until_expiry": daysUntil,
				"expires_at":        d.ExpiresAt.Format("2006-01-02"),
			},
			EvidenceURI: []string{d.BlobURI},
		}
		if err := s.Anomaly.Open(ctx, a); err != nil {
			s.Logger.Warn("open expiring anomaly", "doc_id", d.ID, "err", err)
			continue
		}
		observability.RecordComplianceDocExpiring(ctx, d.VendorID, daysUntil)
		handled++
	}

	if handled > 0 {
		s.Logger.Info("doc-expiry scan",
			"handled", handled,
			"past", len(pastExpiry),
			"soon", len(expiringSoon),
		)
	}
	return handled, nil
}

// Run loops until ctx cancellation, calling RunOnce every Interval.
func (s *DocumentExpiryScanner) Run(ctx context.Context) error {
	if s.Interval <= 0 {
		s.Interval = time.Hour
	}
	if s.DaysWindow <= 0 {
		s.DaysWindow = 14
	}
	s.Logger.Info("doc-expiry scanner started", "interval", s.Interval, "days_window", s.DaysWindow)
	if _, err := s.RunOnce(ctx); err != nil {
		s.Logger.Error("initial scan", "err", err)
	}
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.Logger.Info("doc-expiry scanner stopping")
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				s.Logger.Error("tick scan", "err", err)
			}
		}
	}
}
