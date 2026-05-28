package compliance

import (
	"context"
	"time"

	vendor "github.com/Agentic-Build/corporate-catering-system/services/api/internal/vendors"
)

// VendorReader is the minimal vendor read dependency the merchant compliance
// self-view needs. *vendors/postgres.VendorRepo satisfies it.
type VendorReader interface {
	GetByID(ctx context.Context, id string) (*vendor.Vendor, error)
}

// Warning kinds surfaced by the merchant compliance self-view.
const (
	WarningDocumentRejected = "document_rejected"
	WarningDocumentExpired  = "document_expired"
	WarningDocumentExpiring = "document_expiring"
	WarningDocumentMissing  = "document_missing"
)

// expiringWindowDays is the look-ahead window for a "document_expiring" warning.
const expiringWindowDays = 30

// requiredDocumentKinds are the document kinds every vendor must have on file.
// A required kind with no uploaded document raises a document_missing warning.
var requiredDocumentKinds = []DocumentKind{
	DocKindBusinessLicense,
	DocKindFoodSafetyPermit,
	DocKindTaxRegistration,
	DocKindInsurance,
}

// Warning is a single compliance hint computed for the merchant self-view.
type Warning struct {
	Kind     string
	Message  string
	Severity AnomalySeverity
}

// VendorInfo is the slim vendor snapshot returned by the self-view.
type VendorInfo struct {
	ID          string
	DisplayName string
	Status      string
}

// MerchantComplianceSummary is the read-only payload a vendor sees about its
// own compliance standing.
type MerchantComplianceSummary struct {
	Vendor    VendorInfo
	Documents []*Document
	Warnings  []Warning
}

// MerchantCompliance assembles the read-only compliance self-view for the
// given vendor: vendor status, its documents, and computed warnings.
func (s *Service) MerchantCompliance(ctx context.Context, vendorID string) (*MerchantComplianceSummary, error) {
	v, err := s.Vendors.GetByID(ctx, vendorID)
	if err != nil {
		return nil, err
	}
	docs, err := s.ListVendorDocuments(ctx, vendorID, false)
	if err != nil {
		return nil, err
	}
	return &MerchantComplianceSummary{
		Vendor: VendorInfo{
			ID:          v.ID,
			DisplayName: v.DisplayName,
			Status:      string(v.Status),
		},
		Documents: docs,
		Warnings:  computeWarnings(docs, s.Clock.Now()),
	}, nil
}

// computeWarnings derives compliance warnings from a vendor's document set.
// Pure: same inputs → same warnings. Kinds: document_rejected (any rejected),
// document_expired / document_expiring (approved within expiringWindowDays),
// document_missing (required kind absent).
func computeWarnings(docs []*Document, now time.Time) []Warning {
	var warnings []Warning
	uploaded := make(map[DocumentKind]bool, len(docs))
	cutoff := now.AddDate(0, 0, expiringWindowDays)

	for _, d := range docs {
		// DESIGN DECISION (reviewers: comment here if you disagree) — a required
		// kind counts as "on file" as soon as ANY document of that kind exists,
		// whatever its status. So a vendor whose only business_license was
		// rejected gets a document_rejected warning, NOT document_missing: the
		// document is on file and the rejected warning already tells them to
		// fix it. If a rejected required doc should instead also count as
		// missing, gate this on d.Status (e.g. only approved/pending qualify).
		uploaded[d.Kind] = true

		if d.Status == DocStatusRejected {
			warnings = append(warnings, Warning{
				Kind:     WarningDocumentRejected,
				Message:  "文件「" + string(d.Kind) + "」已被駁回，請更正後重新提交。",
				Severity: SeverityHigh,
			})
		}

		if d.Status != DocStatusApproved || d.ExpiresAt == nil {
			continue
		}
		switch {
		case d.ExpiresAt.Before(now):
			warnings = append(warnings, Warning{
				Kind:     WarningDocumentExpired,
				Message:  "文件「" + string(d.Kind) + "」已過期，請儘速更新。",
				Severity: SeverityHigh,
			})
		case !d.ExpiresAt.After(cutoff):
			warnings = append(warnings, Warning{
				Kind:     WarningDocumentExpiring,
				Message:  "文件「" + string(d.Kind) + "」將於 30 天內到期，請提前準備更新。",
				Severity: SeverityMedium,
			})
		}
	}

	for _, kind := range requiredDocumentKinds {
		if !uploaded[kind] {
			warnings = append(warnings, Warning{
				Kind:     WarningDocumentMissing,
				Message:  "必繳文件「" + string(kind) + "」尚未上傳。",
				Severity: SeverityHigh,
			})
		}
	}

	return warnings
}
