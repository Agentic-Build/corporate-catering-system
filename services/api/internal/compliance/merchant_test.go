package compliance

import (
	"sort"
	"testing"
	"time"
)

// day returns midnight UTC for the given offset (in days) from base.
func day(base time.Time, offsetDays int) time.Time {
	return base.AddDate(0, 0, offsetDays)
}

// warningKinds extracts the kind of each warning, sorted, for stable assertions.
func warningKinds(ws []Warning) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Kind
	}
	sort.Strings(out)
	return out
}

func hasKind(ws []Warning, kind string) bool {
	for _, w := range ws {
		if w.Kind == kind {
			return true
		}
	}
	return false
}

func TestComputeWarnings_AllRequiredApproved_NoWarnings(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if len(got) != 0 {
		t.Fatalf("expected no warnings, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_MissingRequiredKinds(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	// Only business_license uploaded; the other three required kinds missing.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	missing := 0
	for _, w := range got {
		if w.Kind == WarningDocumentMissing {
			missing++
		}
	}
	if missing != 3 {
		t.Fatalf("expected 3 document_missing warnings, got %d (%v)", missing, warningKinds(got))
	}
}

func TestComputeWarnings_MissingTreatsNonApprovedAsPresent(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	// A pending business_license still counts as "uploaded" — no missing warning for it.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusPending},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if hasKind(got, WarningDocumentMissing) {
		t.Fatalf("did not expect document_missing, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Rejected(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusRejected},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if !hasKind(got, WarningDocumentRejected) {
		t.Fatalf("expected document_rejected, got %v", warningKinds(got))
	}
	// A rejected required kind is also "not approved" but still uploaded — no missing.
	if hasKind(got, WarningDocumentMissing) {
		t.Fatalf("did not expect document_missing for a rejected (uploaded) kind, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Expired(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	past := day(now, -1)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &past},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if !hasKind(got, WarningDocumentExpired) {
		t.Fatalf("expected document_expired, got %v", warningKinds(got))
	}
	if hasKind(got, WarningDocumentExpiring) {
		t.Fatalf("an already-expired doc should not also be document_expiring, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Expiring_WithinThirtyDays(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	soon := day(now, 10)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &soon},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if !hasKind(got, WarningDocumentExpiring) {
		t.Fatalf("expected document_expiring, got %v", warningKinds(got))
	}
	if hasKind(got, WarningDocumentExpired) {
		t.Fatalf("did not expect document_expired, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Expiring_BoundaryAt30Days(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	at30 := day(now, 30)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &at30},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if !hasKind(got, WarningDocumentExpiring) {
		t.Fatalf("a doc expiring exactly 30 days out should warn, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Expiring_JustOutsideWindow(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	at31 := day(now, 31)
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved, ExpiresAt: &at31},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if hasKind(got, WarningDocumentExpiring) {
		t.Fatalf("a doc expiring 31 days out should not warn, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_ExpiryIgnoresNonApproved(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	past := day(now, -1)
	soon := day(now, 5)
	// A pending doc past expiry / expiring soon must not raise expiry warnings;
	// only approved docs have a meaningful expiry.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusPending, ExpiresAt: &past},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusPending, ExpiresAt: &soon},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if hasKind(got, WarningDocumentExpired) || hasKind(got, WarningDocumentExpiring) {
		t.Fatalf("expiry warnings should only consider approved docs, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_ApprovedWithoutExpiry_NoExpiryWarning(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	// All approved, none has an expires_at — perpetual docs raise nothing.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusApproved},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved},
		{Kind: DocKindInsurance, Status: DocStatusApproved},
	}
	got := computeWarnings(docs, now)
	if len(got) != 0 {
		t.Fatalf("expected no warnings for approved docs without expiry, got %v", warningKinds(got))
	}
}

func TestComputeWarnings_Combined(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	past := day(now, -3)
	soon := day(now, 14)
	// business_license rejected, food_safety_permit expired, tax_registration
	// expiring soon, insurance entirely missing.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusRejected},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &past},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &soon},
	}
	got := computeWarnings(docs, now)
	for _, want := range []string{
		WarningDocumentRejected,
		WarningDocumentExpired,
		WarningDocumentExpiring,
		WarningDocumentMissing,
	} {
		if !hasKind(got, want) {
			t.Fatalf("expected warning %q in %v", want, warningKinds(got))
		}
	}
}

func TestComputeWarnings_IgnoresExpiredStatusDocs(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	far := day(now, 365)
	past := day(now, -10)
	// A doc whose status is already 'expired' is uploaded → no missing warning,
	// and not approved → no expired/expiring warning. It only fails when rejected.
	docs := []*Document{
		{Kind: DocKindBusinessLicense, Status: DocStatusExpired, ExpiresAt: &past},
		{Kind: DocKindFoodSafetyPermit, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindTaxRegistration, Status: DocStatusApproved, ExpiresAt: &far},
		{Kind: DocKindInsurance, Status: DocStatusApproved, ExpiresAt: &far},
	}
	got := computeWarnings(docs, now)
	if len(got) != 0 {
		t.Fatalf("expected no warnings for an expired-status doc, got %v", warningKinds(got))
	}
}
