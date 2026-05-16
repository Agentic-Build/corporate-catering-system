package feedback

import "time"

// ComplaintCategory mirrors the meal_complaint_category enum.
type ComplaintCategory string

const (
	CategoryWrongItem   ComplaintCategory = "wrong_item"
	CategoryMissingItem ComplaintCategory = "missing_item"
	CategoryQuality     ComplaintCategory = "quality"
	CategoryPortion     ComplaintCategory = "portion"
	CategoryHygiene     ComplaintCategory = "hygiene"
	CategoryOther       ComplaintCategory = "other"
)

// ComplaintStatus mirrors the meal_complaint_status enum and drives the
// complaint workflow state machine (see service.go).
type ComplaintStatus string

const (
	StatusOpen            ComplaintStatus = "open"
	StatusVendorResponded ComplaintStatus = "vendor_responded"
	StatusEscalated       ComplaintStatus = "escalated"
	StatusResolved        ComplaintStatus = "resolved"
)

// Rating is a per-order meal score (1-5) with an optional comment.
type Rating struct {
	ID        string
	OrderID   string
	UserID    string
	VendorID  string
	Score     int
	Comment   string
	CreatedAt time.Time
}

// Complaint is a workflow entity tracking an employee meal complaint through
// open → vendor_responded / escalated → resolved.
type Complaint struct {
	ID                string
	OrderID           string
	UserID            string
	VendorID          string
	Category          ComplaintCategory
	Description       string
	Status            ComplaintStatus
	VendorResponse    string
	VendorRespondedAt *time.Time
	EscalatedAt       *time.Time
	Resolution        string
	ResolvedBy        *string
	ResolvedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// VendorRatingStat is a per-vendor satisfaction aggregate over a rolling
// window. Used by FeedbackScanner.
type VendorRatingStat struct {
	VendorID    string
	AvgScore    float64
	SampleCount int
}

// VendorComplaintStat is a per-vendor complaint-count aggregate over a rolling
// window. Used by FeedbackScanner.
type VendorComplaintStat struct {
	VendorID string
	Count    int
}
