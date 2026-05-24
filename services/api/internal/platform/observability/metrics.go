package observability

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Metrics holds every named instrument the API process emits. A single global
// instance is initialised by MustInitMetrics() after the MeterProvider is
// wired (or after the no-op fallback is set). Call sites use the helper
// functions on this package (RecordOrderPlaced, RecordQuotaExhausted, …) so
// callers don't need to thread a *Metrics through every method.
type Metrics struct {
	OrderPlaced            metric.Int64Counter
	OrderCancelled         metric.Int64Counter
	OrderModified          metric.Int64Counter
	OrderReady             metric.Int64Counter
	OrderPickupVerified    metric.Int64Counter
	OrderNoShow            metric.Int64Counter
	OrderPlaceDurationSec  metric.Float64Histogram
	OrderPriceMinor        metric.Int64Histogram
	QuotaExhausted         metric.Int64Counter
	SupplyAdjusted         metric.Int64Counter
	SupplyAdjustedQty      metric.Int64Histogram
	SettlementRunCount     metric.Int64Counter
	SettlementRunDurSec    metric.Float64Histogram
	SettlementAmountMinor  metric.Int64Histogram
	PayrollEntryAmount     metric.Int64Histogram
	PayrollDispute         metric.Int64Counter
	PayrollReversal        metric.Int64Counter
	ComplianceViolation    metric.Int64Counter
	ComplianceDocExpiring  metric.Int64Counter
	MCPToolInvocation      metric.Int64Counter
	MCPToolDurationSec     metric.Float64Histogram
	MCPToolSideEffects     metric.Int64Counter
	MCPAuthFailure         metric.Int64Counter
}

var (
	metricsOnce sync.Once
	metrics     *Metrics
)

// MustInitMetrics builds every instrument and panics on the first construction
// error. Constructing a metric instrument can only fail when the API contract
// of the SDK is misused (duplicate name/type, invalid unit, ...). Crashing
// loudly is preferable to silently emitting nothing.
func MustInitMetrics() *Metrics {
	metricsOnce.Do(func() {
		m := otel.Meter("tbite.api")
		mustCounter := func(name, desc, unit string) metric.Int64Counter {
			c, err := m.Int64Counter(name, metric.WithDescription(desc), metric.WithUnit(unit))
			if err != nil {
				panic(err)
			}
			return c
		}
		mustFloatHist := func(name, desc, unit string) metric.Float64Histogram {
			h, err := m.Float64Histogram(name, metric.WithDescription(desc), metric.WithUnit(unit))
			if err != nil {
				panic(err)
			}
			return h
		}
		mustIntHist := func(name, desc, unit string) metric.Int64Histogram {
			h, err := m.Int64Histogram(name, metric.WithDescription(desc), metric.WithUnit(unit))
			if err != nil {
				panic(err)
			}
			return h
		}
		metrics = &Metrics{
			OrderPlaced:           mustCounter("catering.order.placed.count", "Orders successfully placed", "1"),
			OrderCancelled:        mustCounter("catering.order.cancelled.count", "Orders cancelled by user or admin", "1"),
			OrderModified:         mustCounter("catering.order.modified.count", "Order item-set replacements before cutoff", "1"),
			OrderReady:            mustCounter("catering.order.ready.count", "Orders marked READY by vendor", "1"),
			OrderPickupVerified:   mustCounter("catering.order.pickup_verified.count", "Pickup verifications", "1"),
			OrderNoShow:           mustCounter("catering.order.no_show.count", "Orders transitioned to NO_SHOW", "1"),
			OrderPlaceDurationSec: mustFloatHist("catering.order.place.duration", "Place() service-method latency", "s"),
			OrderPriceMinor:       mustIntHist("catering.order.price_minor", "Order total price in minor currency units", "1"),
			QuotaExhausted:        mustCounter("catering.quota.exhausted.count", "Place() rejections caused by exhausted quota", "1"),
			SupplyAdjusted:        mustCounter("catering.supply.adjusted.count", "Vendor capacity adjustments", "1"),
			SupplyAdjustedQty:     mustIntHist("catering.supply.adjusted.qty", "Magnitude of vendor capacity adjustments", "1"),
			SettlementRunCount:    mustCounter("catering.settlement.run.count", "Vendor settlement runs", "1"),
			SettlementRunDurSec:   mustFloatHist("catering.settlement.run.duration", "Vendor settlement run latency", "s"),
			SettlementAmountMinor: mustIntHist("catering.settlement.amount_minor", "Settlement amount in minor currency units", "1"),
			PayrollEntryAmount:    mustIntHist("catering.payroll.entry.amount_minor", "Payroll entry net amount in minor currency units", "1"),
			PayrollDispute:        mustCounter("catering.payroll.dispute.count", "Payroll dispute lifecycle events", "1"),
			PayrollReversal:       mustCounter("catering.payroll.reversal.count", "Payroll reversal events", "1"),
			ComplianceViolation:   mustCounter("catering.compliance.violation.count", "Compliance violations or anomalies", "1"),
			ComplianceDocExpiring: mustCounter("catering.compliance.doc_expiring.count", "Vendor documents nearing expiry", "1"),
			MCPToolInvocation:     mustCounter("mcp.tool.invocation.count", "MCP tool invocations", "1"),
			MCPToolDurationSec:    mustFloatHist("mcp.tool.duration", "MCP tool invocation latency", "s"),
			MCPToolSideEffects:    mustCounter("mcp.tool.side_effects.count", "MCP tool invocations grouped by side-effect annotation", "1"),
			MCPAuthFailure:        mustCounter("mcp.auth.failure.count", "MCP authentication failures", "1"),
		}
	})
	return metrics
}

// Get returns the global instrument set. Returns nil if MustInitMetrics has
// not been called; emission helpers below short-circuit safely in that case
// so unit tests don't need to spin up the meter provider.
func Get() *Metrics { return metrics }

// ───────────────────────────── Emission helpers ─────────────────────────────
// Helpers nil-check `metrics` so domain code can call them without an extra
// init dance in every package.

func RecordOrderPlaced(ctx context.Context, plant, vendor, mealWindow, outcome string) {
	if metrics == nil {
		return
	}
	metrics.OrderPlaced.Add(ctx, 1, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
		attribute.String("meal_window", mealWindow),
		attribute.String("outcome", outcome),
	))
}

func RecordOrderCancelled(ctx context.Context, plant, vendor, reason, actorRole string) {
	if metrics == nil {
		return
	}
	metrics.OrderCancelled.Add(ctx, 1, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
		attribute.String("reason", reason),
		attribute.String("actor_role", actorRole),
	))
}

func RecordOrderModified(ctx context.Context, plant, vendor string) {
	if metrics == nil {
		return
	}
	metrics.OrderModified.Add(ctx, 1, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
	))
}

func RecordOrderReady(ctx context.Context, vendor string, count int) {
	if metrics == nil || count <= 0 {
		return
	}
	metrics.OrderReady.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String("vendor_id", vendor),
	))
}

// RecordPickupVerified fires on every verify-pickup attempt with an outcome
// label (success / invalid_code / wrong_state / order_lookup_failed / ...).
// Method is always "totp" today but kept on the metric so a future QR-only
// or admin-override path could be distinguished without a new instrument.
func RecordPickupVerified(ctx context.Context, plant, vendor, outcome string) {
	if metrics == nil {
		return
	}
	metrics.OrderPickupVerified.Add(ctx, 1, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
		attribute.String("method", "totp"),
		attribute.String("outcome", outcome),
	))
}

func RecordOrderNoShow(ctx context.Context, count int) {
	if metrics == nil || count <= 0 {
		return
	}
	metrics.OrderNoShow.Add(ctx, int64(count))
}

func RecordOrderPlaceLatency(ctx context.Context, seconds float64, plant, mealWindow, outcome string) {
	if metrics == nil {
		return
	}
	metrics.OrderPlaceDurationSec.Record(ctx, seconds, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("meal_window", mealWindow),
		attribute.String("outcome", outcome),
	))
}

func RecordOrderPrice(ctx context.Context, minor int64, plant, vendor string) {
	if metrics == nil || minor <= 0 {
		return
	}
	metrics.OrderPriceMinor.Record(ctx, minor, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
	))
}

func RecordQuotaExhausted(ctx context.Context, plant, vendor, mealWindow, menuItem string) {
	if metrics == nil {
		return
	}
	metrics.QuotaExhausted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("plant_id", plant),
		attribute.String("vendor_id", vendor),
		attribute.String("meal_window", mealWindow),
		attribute.String("menu_item_id", menuItem),
	))
}

func RecordSupplyAdjusted(ctx context.Context, vendor, direction string, deltaAbs int) {
	if metrics == nil {
		return
	}
	metrics.SupplyAdjusted.Add(ctx, 1, metric.WithAttributes(
		attribute.String("vendor_id", vendor),
		attribute.String("direction", direction),
	))
	if deltaAbs > 0 {
		metrics.SupplyAdjustedQty.Record(ctx, int64(deltaAbs), metric.WithAttributes(
			attribute.String("vendor_id", vendor),
			attribute.String("direction", direction),
		))
	}
}

func RecordSettlementRun(ctx context.Context, vendor, outcome string, durSec float64, amountMinor int64) {
	if metrics == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("vendor_id", vendor),
		attribute.String("outcome", outcome),
	}
	metrics.SettlementRunCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.SettlementRunDurSec.Record(ctx, durSec, metric.WithAttributes(attrs...))
	if amountMinor > 0 {
		metrics.SettlementAmountMinor.Record(ctx, amountMinor, metric.WithAttributes(attrs...))
	}
}

func RecordPayrollEntry(ctx context.Context, period string, amountMinor int64) {
	if metrics == nil {
		return
	}
	metrics.PayrollEntryAmount.Record(ctx, amountMinor, metric.WithAttributes(
		attribute.String("period", period),
	))
}

func RecordPayrollDispute(ctx context.Context, action string) {
	if metrics == nil {
		return
	}
	metrics.PayrollDispute.Add(ctx, 1, metric.WithAttributes(
		attribute.String("action", action),
	))
}

func RecordPayrollReversal(ctx context.Context, reason string) {
	if metrics == nil {
		return
	}
	metrics.PayrollReversal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
	))
}

func RecordComplianceViolation(ctx context.Context, ruleID, severity, vendor string) {
	if metrics == nil {
		return
	}
	metrics.ComplianceViolation.Add(ctx, 1, metric.WithAttributes(
		attribute.String("rule_id", ruleID),
		attribute.String("severity", severity),
		attribute.String("vendor_id", vendor),
	))
}

func RecordComplianceDocExpiring(ctx context.Context, vendor string, daysUntilExpiry int) {
	if metrics == nil {
		return
	}
	bucket := "60+"
	switch {
	case daysUntilExpiry <= 7:
		bucket = "<=7"
	case daysUntilExpiry <= 14:
		bucket = "<=14"
	case daysUntilExpiry <= 30:
		bucket = "<=30"
	case daysUntilExpiry <= 60:
		bucket = "<=60"
	}
	metrics.ComplianceDocExpiring.Add(ctx, 1, metric.WithAttributes(
		attribute.String("vendor_id", vendor),
		attribute.String("expiry_bucket", bucket),
	))
}

// MCPToolCall records the three instruments touched by every MCP tool call.
func MCPToolCall(ctx context.Context, tool, clientID, outcome, sideEffect string, seconds float64, log *slog.Logger) {
	if metrics == nil {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("tool_name", tool),
		attribute.String("client_id", clientID),
		attribute.String("outcome", outcome),
	}
	metrics.MCPToolInvocation.Add(ctx, 1, metric.WithAttributes(attrs...))
	metrics.MCPToolDurationSec.Record(ctx, seconds, metric.WithAttributes(attrs...))
	metrics.MCPToolSideEffects.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tool_name", tool),
		attribute.String("annotation", sideEffect),
		attribute.String("outcome", outcome),
	))
	if outcome != "success" && log != nil {
		log.Debug("mcp tool non-success", "tool", tool, "outcome", outcome, "dur_s", seconds)
	}
}

func RecordMCPAuthFailure(ctx context.Context, reason string) {
	if metrics == nil {
		return
	}
	metrics.MCPAuthFailure.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
	))
}
