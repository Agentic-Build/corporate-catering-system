package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	orderStatusPickedUp = "picked_up"

	maxCommentLen     = 500
	minDescriptionLen = 5
	maxDescriptionLen = 1000
	minResponseLen    = 5

	// escalateGate is how long an employee must wait after creating a
	// complaint before they may escalate it to the welfare committee.
	escalateGate = 24 * time.Hour
)

// Clock lets tests pin "now".
type Clock interface{ Now() time.Time }

// Service orchestrates meal ratings and the complaint workflow. Every write
// pairs the domain mutation with an audit_event row inside one transaction.
type Service struct {
	Pool       *pgxpool.Pool
	Ratings    RatingRepository
	Complaints ComplaintRepository
	Orders     OrderReader
	Audit      AuditTx
	Clock      Clock
}

// ----- Rating -----

// RateOrderInput captures an employee's meal rating for one picked-up order.
type RateOrderInput struct {
	OrderID string
	UserID  string
	Score   int
	Comment string
}

// RateOrder records a 1-5 score (+ optional comment) for a picked-up order
// the employee owns. A second rating for the same order returns ErrAlreadyRated.
func (s *Service) RateOrder(ctx context.Context, in RateOrderInput) (*Rating, error) {
	if in.Score < 1 || in.Score > 5 {
		return nil, fmt.Errorf("%w: score must be between 1 and 5", ErrValidation)
	}
	if len(in.Comment) > maxCommentLen {
		return nil, fmt.Errorf("%w: comment must be at most %d characters", ErrValidation, maxCommentLen)
	}

	o, err := s.Orders.GetOrderInfo(ctx, in.OrderID)
	if err != nil {
		return nil, err
	}
	if o.UserID != in.UserID {
		return nil, ErrForbidden
	}
	if o.Status != orderStatusPickedUp {
		return nil, ErrOrderNotPickedUp
	}

	if existing, err := s.Ratings.GetByOrder(ctx, in.OrderID); err != nil {
		if !errors.Is(err, ErrRatingNotFound) {
			return nil, err
		}
	} else if existing != nil {
		return nil, ErrAlreadyRated
	}

	r := &Rating{
		OrderID:  in.OrderID,
		UserID:   in.UserID,
		VendorID: o.VendorID,
		Score:    in.Score,
		Comment:  in.Comment,
	}
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Ratings.CreateTx(ctx, tx, r); err != nil {
			return mapUniqueViolation(err)
		}
		return s.writeAudit(ctx, tx, in.UserID, "employee", "feedback.rate_order", "meal_rating", r.ID, map[string]any{
			"order_id":  in.OrderID,
			"vendor_id": o.VendorID,
			"score":     in.Score,
		})
	})
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetRating returns the rating for an order, or ErrRatingNotFound.
func (s *Service) GetRating(ctx context.Context, orderID string) (*Rating, error) {
	return s.Ratings.GetByOrder(ctx, orderID)
}

// ----- Complaint: creation -----

// FileComplaintInput captures an employee's complaint about one picked-up order.
type FileComplaintInput struct {
	OrderID     string
	UserID      string
	Category    ComplaintCategory
	Description string
}

// FileComplaint opens a new complaint (status=open) for a picked-up order the
// employee owns. At most one unresolved complaint may exist per order; a
// second one returns ErrComplaintExists.
func (s *Service) FileComplaint(ctx context.Context, in FileComplaintInput) (*Complaint, error) {
	if !validCategory(in.Category) {
		return nil, fmt.Errorf("%w: invalid category", ErrValidation)
	}
	desc := strings.TrimSpace(in.Description)
	if len(desc) < minDescriptionLen || len(desc) > maxDescriptionLen {
		return nil, fmt.Errorf("%w: description must be %d-%d characters", ErrValidation, minDescriptionLen, maxDescriptionLen)
	}

	o, err := s.Orders.GetOrderInfo(ctx, in.OrderID)
	if err != nil {
		return nil, err
	}
	if o.UserID != in.UserID {
		return nil, ErrForbidden
	}
	if o.Status != orderStatusPickedUp {
		return nil, ErrOrderNotPickedUp
	}

	c := &Complaint{
		OrderID:     in.OrderID,
		UserID:      in.UserID,
		VendorID:    o.VendorID,
		Category:    in.Category,
		Description: desc,
		Status:      StatusOpen,
	}
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Complaints.CreateTx(ctx, tx, c); err != nil {
			return mapUniqueViolation(err)
		}
		return s.writeAudit(ctx, tx, in.UserID, "employee", "feedback.file_complaint", "meal_complaint", c.ID, map[string]any{
			"order_id":  in.OrderID,
			"vendor_id": o.VendorID,
			"category":  string(in.Category),
		})
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ----- Complaint: workflow transitions -----

// RespondToComplaint is the vendor action: open → vendor_responded. The vendor
// must own the complaint's vendor; response text must be at least 5 chars.
func (s *Service) RespondToComplaint(ctx context.Context, complaintID, vendorID, actorUserID, response string) error {
	c, err := s.Complaints.GetByID(ctx, complaintID)
	if err != nil {
		return err
	}
	if c.VendorID != vendorID {
		return ErrForbidden
	}
	if c.Status != StatusOpen {
		return ErrInvalidTransition
	}
	if len(strings.TrimSpace(response)) < minResponseLen {
		return fmt.Errorf("%w: response must be at least %d characters", ErrValidation, minResponseLen)
	}
	return s.transition(ctx, c, StatusOpen, StatusVendorResponded,
		ComplaintUpdate{VendorResponse: strings.TrimSpace(response)},
		actorUserID, "vendor_operator", "feedback.complaint_respond",
		map[string]any{"vendor_id": vendorID})
}

// EscalateComplaint is the employee action: open|vendor_responded → escalated.
// It is gated: the complaint must be at least 24h old. The employee must own
// the complaint.
func (s *Service) EscalateComplaint(ctx context.Context, complaintID, userID string) error {
	c, err := s.Complaints.GetByID(ctx, complaintID)
	if err != nil {
		return err
	}
	if c.UserID != userID {
		return ErrForbidden
	}
	if c.Status != StatusOpen && c.Status != StatusVendorResponded {
		return ErrInvalidTransition
	}
	if s.Clock.Now().Before(c.CreatedAt.Add(escalateGate)) {
		return ErrEscalateTooEarly
	}
	return s.transition(ctx, c, c.Status, StatusEscalated,
		ComplaintUpdate{},
		userID, "employee", "feedback.complaint_escalate",
		map[string]any{"from": string(c.Status)})
}

// EmployeeResolveComplaint is the employee "satisfied" close: open|
// vendor_responded → resolved. The employee must own the complaint.
func (s *Service) EmployeeResolveComplaint(ctx context.Context, complaintID, userID string) error {
	c, err := s.Complaints.GetByID(ctx, complaintID)
	if err != nil {
		return err
	}
	if c.UserID != userID {
		return ErrForbidden
	}
	if c.Status != StatusOpen && c.Status != StatusVendorResponded {
		return ErrInvalidTransition
	}
	return s.transition(ctx, c, c.Status, StatusResolved,
		ComplaintUpdate{Resolution: "resolved by employee (satisfied)", ResolvedBy: &userID},
		userID, "employee", "feedback.complaint_resolve",
		map[string]any{"resolved_by_role": "employee", "from": string(c.Status)})
}

// AdminResolveComplaint is the welfare-committee close of an escalated
// complaint: escalated → resolved. Resolution text must be at least 5 chars.
func (s *Service) AdminResolveComplaint(ctx context.Context, complaintID, adminUserID, resolution string) error {
	c, err := s.Complaints.GetByID(ctx, complaintID)
	if err != nil {
		return err
	}
	if c.Status != StatusEscalated {
		return ErrInvalidTransition
	}
	if len(strings.TrimSpace(resolution)) < minResponseLen {
		return fmt.Errorf("%w: resolution must be at least %d characters", ErrValidation, minResponseLen)
	}
	return s.transition(ctx, c, StatusEscalated, StatusResolved,
		ComplaintUpdate{Resolution: strings.TrimSpace(resolution), ResolvedBy: &adminUserID},
		adminUserID, "welfare_admin", "feedback.complaint_resolve",
		map[string]any{"resolved_by_role": "welfare_admin"})
}

// transition applies a complaint status change + audit row in one transaction.
func (s *Service) transition(ctx context.Context, c *Complaint, from, to ComplaintStatus, fields ComplaintUpdate, actorID, actorRole, action string, extra map[string]any) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if err := s.Complaints.UpdateStatusTx(ctx, tx, c.ID, from, to, fields); err != nil {
			return err
		}
		payload := map[string]any{
			"complaint_id": c.ID,
			"order_id":     c.OrderID,
			"vendor_id":    c.VendorID,
			"to":           string(to),
		}
		for k, v := range extra {
			payload[k] = v
		}
		return s.writeAudit(ctx, tx, actorID, actorRole, action, "meal_complaint", c.ID, payload)
	})
}

// ----- Queries -----

// ListMyComplaints returns complaints filed by an employee.
func (s *Service) ListMyComplaints(ctx context.Context, userID string) ([]*Complaint, error) {
	return s.Complaints.ListByUser(ctx, userID)
}

// ListVendorComplaints returns a vendor's complaint inbox, optionally filtered
// by status.
func (s *Service) ListVendorComplaints(ctx context.Context, vendorID string, statuses []ComplaintStatus) ([]*Complaint, error) {
	return s.Complaints.ListByVendor(ctx, vendorID, statuses)
}

// ListEscalatedComplaints returns complaints escalated to the welfare committee.
func (s *Service) ListEscalatedComplaints(ctx context.Context) ([]*Complaint, error) {
	return s.Complaints.ListByStatus(ctx, []ComplaintStatus{StatusEscalated})
}

// GetComplaint fetches a single complaint by id.
func (s *Service) GetComplaint(ctx context.Context, id string) (*Complaint, error) {
	return s.Complaints.GetByID(ctx, id)
}

// ----- helpers -----

func (s *Service) writeAudit(ctx context.Context, tx pgx.Tx, actorID, actorRole, action, targetKind, targetID string, payload map[string]any) error {
	aID := actorID
	aRole := actorRole
	return s.Audit.WriteTx(ctx, tx, &aID, &aRole, action, targetKind, targetID, payload, "")
}

func validCategory(c ComplaintCategory) bool {
	switch c {
	case CategoryWrongItem, CategoryMissingItem, CategoryQuality,
		CategoryPortion, CategoryHygiene, CategoryOther:
		return true
	}
	return false
}

// mapUniqueViolation translates the partial-unique-index / unique-constraint
// errors into typed feedback sentinels so handlers can return a clean 409.
func mapUniqueViolation(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "meal_complaint_one_open_idx"):
		return ErrComplaintExists
	case strings.Contains(msg, "meal_rating_order_id_key"):
		return ErrAlreadyRated
	}
	return err
}
