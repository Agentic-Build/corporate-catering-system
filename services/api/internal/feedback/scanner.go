package feedback

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/compliance"
)

// scanner thresholds (design §3.5).
const (
	defaultScanInterval = time.Hour
	defaultScanWindow   = 14 * 24 * time.Hour

	minRatingSamples = 5
	satWarnThreshold = 3.5 // avg below this opens satisfaction_drop
	satHighThreshold = 2.5 // avg below this escalates severity to high

	complaintWarnThreshold = 5  // count at/above this opens complaint_spike
	complaintHighThreshold = 10 // count at/above this escalates to high

	anomalyKindSatisfactionDrop = "satisfaction_drop"
	anomalyKindComplaintSpike   = "complaint_spike"
	anomalyTargetKindVendor     = "vendor"
)

// FeedbackScanner periodically aggregates meal ratings and complaints over a
// rolling window and opens compliance anomalies for vendors whose satisfaction
// has dropped or whose complaint volume has spiked. Dedup is handled by the
// existing anomaly_alert partial unique index, so opening the same anomaly
// repeatedly just refreshes payload + severity.
//
// It mirrors compliance/scanner.DocumentExpiryScanner: a RunOnce single
// pass plus a Run interval loop, running in the --role=feedback-scanner
// singleton (one of the split scheduler roles introduced by
// architecture #56).
type FeedbackScanner struct {
	Ratings    RatingRepository
	Complaints ComplaintRepository
	Anomaly    compliance.AnomalyRepository
	Clock      Nower
	Logger     *slog.Logger

	// Interval is how often Run triggers a scan (default 1h).
	Interval time.Duration
	// Window is the rolling lookback applied to ratings/complaints (default 14d).
	Window time.Duration
}

// openRatingAnomalies opens satisfaction_drop anomalies for vendors whose
// sample-count is above floor AND avg_score is below the warn threshold.
func (s *FeedbackScanner) openRatingAnomalies(ctx context.Context, stats []VendorRatingStat, windowDays int) int {
	opened := 0
	for _, st := range stats {
		if st.SampleCount < minRatingSamples || st.AvgScore >= satWarnThreshold {
			continue
		}
		sev := compliance.SeverityMedium
		if st.AvgScore < satHighThreshold {
			sev = compliance.SeverityHigh
		}
		a := &compliance.Anomaly{
			Kind:       anomalyKindSatisfactionDrop,
			TargetKind: anomalyTargetKindVendor,
			TargetID:   st.VendorID,
			Severity:   sev,
			Payload: map[string]any{
				"avg_score":    st.AvgScore,
				"sample_count": st.SampleCount,
				"window_days":  windowDays,
				"threshold":    satWarnThreshold,
			},
		}
		if err := s.Anomaly.Open(ctx, a); err != nil {
			s.logWarn("open satisfaction_drop anomaly", "vendor_id", st.VendorID, "err", err)
			continue
		}
		opened++
	}
	return opened
}

// openComplaintAnomalies opens complaint_spike anomalies for vendors whose
// complaint count exceeds the warn threshold.
func (s *FeedbackScanner) openComplaintAnomalies(ctx context.Context, stats []VendorComplaintStat, windowDays int) int {
	opened := 0
	for _, st := range stats {
		if st.Count < complaintWarnThreshold {
			continue
		}
		sev := compliance.SeverityMedium
		if st.Count >= complaintHighThreshold {
			sev = compliance.SeverityHigh
		}
		a := &compliance.Anomaly{
			Kind:       anomalyKindComplaintSpike,
			TargetKind: anomalyTargetKindVendor,
			TargetID:   st.VendorID,
			Severity:   sev,
			Payload: map[string]any{
				"complaint_count": st.Count,
				"window_days":     windowDays,
				"threshold":       complaintWarnThreshold,
			},
		}
		if err := s.Anomaly.Open(ctx, a); err != nil {
			s.logWarn("open complaint_spike anomaly", "vendor_id", st.VendorID, "err", err)
			continue
		}
		opened++
	}
	return opened
}

// RunOnce executes a single scan pass. Returns the number of anomalies opened
// (or refreshed) across both signals.
func (s *FeedbackScanner) RunOnce(ctx context.Context) (int, error) {
	window := s.Window
	if window <= 0 {
		window = defaultScanWindow
	}
	now := s.Clock.Now().UTC()
	since := now.Add(-window)
	windowDays := int(window.Hours() / 24)

	ratingStats, err := s.Ratings.AggregateByVendorSince(ctx, since)
	if err != nil {
		return 0, fmt.Errorf("aggregate ratings: %w", err)
	}
	opened := s.openRatingAnomalies(ctx, ratingStats, windowDays)

	complaintStats, err := s.Complaints.CountByVendorSince(ctx, since)
	if err != nil {
		return opened, fmt.Errorf("count complaints: %w", err)
	}
	opened += s.openComplaintAnomalies(ctx, complaintStats, windowDays)

	if opened > 0 {
		s.logInfo("feedback scan",
			"opened", opened,
			"rating_vendors", len(ratingStats),
			"complaint_vendors", len(complaintStats),
		)
	}
	return opened, nil
}

// Run loops until ctx cancellation, calling RunOnce every Interval.
func (s *FeedbackScanner) Run(ctx context.Context) error {
	if s.Interval <= 0 {
		s.Interval = defaultScanInterval
	}
	if s.Window <= 0 {
		s.Window = defaultScanWindow
	}
	s.logInfo("feedback scanner started", "interval", s.Interval, "window", s.Window)
	if _, err := s.RunOnce(ctx); err != nil {
		s.logError("initial scan", "err", err)
	}
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logInfo("feedback scanner stopping")
			return ctx.Err()
		case <-ticker.C:
			if _, err := s.RunOnce(ctx); err != nil {
				s.logError("tick scan", "err", err)
			}
		}
	}
}

func (s *FeedbackScanner) logInfo(msg string, args ...any) {
	if s.Logger != nil {
		s.Logger.Info(msg, args...)
	}
}

func (s *FeedbackScanner) logWarn(msg string, args ...any) {
	if s.Logger != nil {
		s.Logger.Warn(msg, args...)
	}
}

func (s *FeedbackScanner) logError(msg string, args ...any) {
	if s.Logger != nil {
		s.Logger.Error(msg, args...)
	}
}
