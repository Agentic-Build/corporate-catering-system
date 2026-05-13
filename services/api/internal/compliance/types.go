package compliance

import "time"

type DocumentKind string

const (
	DocKindBusinessLicense  DocumentKind = "business_license"
	DocKindFoodSafetyPermit DocumentKind = "food_safety_permit"
	DocKindTaxRegistration  DocumentKind = "tax_registration"
	DocKindInsurance        DocumentKind = "insurance"
	DocKindOther            DocumentKind = "other"
)

type DocumentStatus string

const (
	DocStatusPending  DocumentStatus = "pending"
	DocStatusApproved DocumentStatus = "approved"
	DocStatusRejected DocumentStatus = "rejected"
	DocStatusExpired  DocumentStatus = "expired"
)

type AnomalySeverity string

const (
	SeverityLow      AnomalySeverity = "low"
	SeverityMedium   AnomalySeverity = "medium"
	SeverityHigh     AnomalySeverity = "high"
	SeverityCritical AnomalySeverity = "critical"
)

type AnomalyStatus string

const (
	AnomalyOpen    AnomalyStatus = "open"
	AnomalyTriaged AnomalyStatus = "triaged"
	AnomalyClosed  AnomalyStatus = "closed"
)

type Document struct {
	ID         string
	VendorID   string
	Kind       DocumentKind
	BlobURI    string
	Filename   string
	UploadedBy *string
	ExpiresAt  *time.Time
	Status     DocumentStatus
	ReviewedBy *string
	ReviewedAt *time.Time
	Notes      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type Anomaly struct {
	ID          string
	Kind        string
	TargetKind  string
	TargetID    string
	Severity    AnomalySeverity
	Status      AnomalyStatus
	Payload     map[string]any
	EvidenceURI []string
	TriagedAt   *time.Time
	TriagedBy   *string
	ClosedAt    *time.Time
	ClosedBy    *string
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
