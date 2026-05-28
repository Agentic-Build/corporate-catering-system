// Package feedbackhttp wires the feedback Service to huma endpoints: employee
// meal ratings + complaint filing, the vendor complaint inbox, and the welfare
// committee escalated-complaint queue.
package feedbackhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/feedback"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// API exposes feedback endpoints across employee / vendor / admin roles.
type API struct {
	Svc *feedback.Service
}

type ratingDTO struct {
	ID        string `json:"id"`
	OrderID   string `json:"order_id"`
	UserID    string `json:"user_id"`
	VendorID  string `json:"vendor_id"`
	Score     int    `json:"score"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"created_at"`
}

type complaintDTO struct {
	ID                string  `json:"id"`
	OrderID           string  `json:"order_id"`
	UserID            string  `json:"user_id"`
	VendorID          string  `json:"vendor_id"`
	Category          string  `json:"category"`
	Description       string  `json:"description"`
	Status            string  `json:"status"`
	VendorResponse    string  `json:"vendor_response"`
	VendorRespondedAt *string `json:"vendor_responded_at,omitempty"`
	EscalatedAt       *string `json:"escalated_at,omitempty"`
	Resolution        string  `json:"resolution"`
	ResolvedBy        *string `json:"resolved_by,omitempty"`
	ResolvedAt        *string `json:"resolved_at,omitempty"`
	CreatedAt         string  `json:"created_at"`
}

func toRatingDTO(r *feedback.Rating) ratingDTO {
	return ratingDTO{
		ID:        r.ID,
		OrderID:   r.OrderID,
		UserID:    r.UserID,
		VendorID:  r.VendorID,
		Score:     r.Score,
		Comment:   r.Comment,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func toComplaintDTO(c *feedback.Complaint) complaintDTO {
	out := complaintDTO{
		ID:             c.ID,
		OrderID:        c.OrderID,
		UserID:         c.UserID,
		VendorID:       c.VendorID,
		Category:       string(c.Category),
		Description:    c.Description,
		Status:         string(c.Status),
		VendorResponse: c.VendorResponse,
		Resolution:     c.Resolution,
		CreatedAt:      c.CreatedAt.UTC().Format(time.RFC3339),
	}
	if c.VendorRespondedAt != nil {
		s := c.VendorRespondedAt.UTC().Format(time.RFC3339)
		out.VendorRespondedAt = &s
	}
	if c.EscalatedAt != nil {
		s := c.EscalatedAt.UTC().Format(time.RFC3339)
		out.EscalatedAt = &s
	}
	if c.ResolvedBy != nil {
		s := *c.ResolvedBy
		out.ResolvedBy = &s
	}
	if c.ResolvedAt != nil {
		s := c.ResolvedAt.UTC().Format(time.RFC3339)
		out.ResolvedAt = &s
	}
	return out
}

type rateOrderInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Score   int    `json:"score" minimum:"1" maximum:"5"`
		Comment string `json:"comment" maxLength:"500"`
	}
}

type ratingOutput struct {
	Body struct {
		Rating ratingDTO `json:"rating"`
	}
}

type getRatingInput struct {
	ID string `path:"id" format:"uuid"`
}

type fileComplaintInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Category    string `json:"category" enum:"wrong_item,missing_item,quality,portion,hygiene,other"`
		Description string `json:"description" minLength:"5" maxLength:"1000"`
	}
}

type complaintOutput struct {
	Body struct {
		Complaint complaintDTO `json:"complaint"`
	}
}

type listComplaintsOutput struct {
	Body struct {
		Items []complaintDTO `json:"items"`
	}
}

type complaintIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type respondComplaintInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Response string `json:"response" minLength:"5"`
	}
}

type listVendorComplaintsInput struct {
	Status string `query:"status" enum:"open,vendor_responded,escalated,resolved,"`
}

type adminResolveInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Resolution string `json:"resolution" minLength:"5"`
		Compensate bool   `json:"compensate,omitempty"`
	}
}

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "rateOrder",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/{id}/rating",
		Summary:       "Submit a meal rating for a picked-up order",
		Tags:          []string{"employee", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.rateOrder)

	huma.Register(api, huma.Operation{
		OperationID: "getMyRating",
		Method:      http.MethodGet,
		Path:        "/api/employee/orders/{id}/rating",
		Summary:     "Get my meal rating for an order",
		Tags:        []string{"employee", "feedback"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getMyRating)

	huma.Register(api, huma.Operation{
		OperationID:   "fileComplaint",
		Method:        http.MethodPost,
		Path:          "/api/employee/orders/{id}/complaint",
		Summary:       "File a complaint for a picked-up order",
		Tags:          []string{"employee", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.fileComplaint)

	huma.Register(api, huma.Operation{
		OperationID: "listMyComplaints",
		Method:      http.MethodGet,
		Path:        "/api/employee/complaints",
		Summary:     "List my filed complaints",
		Tags:        []string{"employee", "feedback"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listMyComplaints)

	huma.Register(api, huma.Operation{
		OperationID:   "escalateComplaint",
		Method:        http.MethodPost,
		Path:          "/api/employee/complaints/{id}/escalate",
		Summary:       "Escalate a complaint to the welfare committee (24h gate)",
		Tags:          []string{"employee", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.escalateComplaint)

	huma.Register(api, huma.Operation{
		OperationID:   "resolveMyComplaint",
		Method:        http.MethodPost,
		Path:          "/api/employee/complaints/{id}/resolve",
		Summary:       "Close my complaint as satisfied",
		Tags:          []string{"employee", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.resolveMyComplaint)

	huma.Register(api, huma.Operation{
		OperationID: "listVendorComplaints",
		Method:      http.MethodGet,
		Path:        "/api/merchant/complaints",
		Summary:     "List complaints for my vendor (the complaint inbox)",
		Tags:        []string{"merchant", "feedback"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listVendorComplaints)

	huma.Register(api, huma.Operation{
		OperationID:   "respondToComplaint",
		Method:        http.MethodPost,
		Path:          "/api/merchant/complaints/{id}/respond",
		Summary:       "Respond to an open complaint",
		Tags:          []string{"merchant", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.respondToComplaint)

	huma.Register(api, huma.Operation{
		OperationID: "listEscalatedComplaints",
		Method:      http.MethodGet,
		Path:        "/api/admin/complaints",
		Summary:     "List complaints escalated to the welfare committee",
		Tags:        []string{"admin", "feedback"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listEscalatedComplaints)

	huma.Register(api, huma.Operation{
		OperationID:   "adminResolveComplaint",
		Method:        http.MethodPost,
		Path:          "/api/admin/complaints/{id}/resolve",
		Summary:       "Resolve an escalated complaint",
		Tags:          []string{"admin", "feedback"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.adminResolveComplaint)
}

func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireEmployee(ctx)
}

func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	return idhttp.RequireVendor(ctx)
}

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	return idhttp.RequireAdmin(ctx)
}

func (a *API) rateOrder(ctx context.Context, in *rateOrderInput) (*ratingOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.RateOrder(ctx, feedback.RateOrderInput{
		OrderID: in.ID,
		UserID:  u.ID,
		Score:   in.Body.Score,
		Comment: in.Body.Comment,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp ratingOutput
	resp.Body.Rating = toRatingDTO(r)
	return &resp, nil
}

func (a *API) getMyRating(ctx context.Context, in *getRatingInput) (*ratingOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	r, err := a.Svc.GetRating(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	// Don't leak another employee's rating: treat as not-found.
	if r.UserID != u.ID {
		return nil, huma.Error404NotFound(feedback.ErrRatingNotFound.Error())
	}
	var resp ratingOutput
	resp.Body.Rating = toRatingDTO(r)
	return &resp, nil
}

func (a *API) fileComplaint(ctx context.Context, in *fileComplaintInput) (*complaintOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	c, err := a.Svc.FileComplaint(ctx, feedback.FileComplaintInput{
		OrderID:     in.ID,
		UserID:      u.ID,
		Category:    feedback.ComplaintCategory(in.Body.Category),
		Description: in.Body.Description,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp complaintOutput
	resp.Body.Complaint = toComplaintDTO(c)
	return &resp, nil
}

func (a *API) listMyComplaints(ctx context.Context, _ *struct{}) (*listComplaintsOutput, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	cs, err := a.Svc.ListMyComplaints(ctx, u.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	return complaintListResponse(cs), nil
}

func (a *API) escalateComplaint(ctx context.Context, in *complaintIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.EscalateComplaint(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) resolveMyComplaint(ctx context.Context, in *complaintIDInput) (*struct{}, error) {
	u, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.EmployeeResolveComplaint(ctx, in.ID, u.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listVendorComplaints(ctx context.Context, in *listVendorComplaintsInput) (*listComplaintsOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	var statuses []feedback.ComplaintStatus
	if in.Status != "" {
		statuses = []feedback.ComplaintStatus{feedback.ComplaintStatus(in.Status)}
	}
	cs, err := a.Svc.ListVendorComplaints(ctx, vendorID, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	return complaintListResponse(cs), nil
}

func (a *API) respondToComplaint(ctx context.Context, in *respondComplaintInput) (*struct{}, error) {
	u, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.RespondToComplaint(ctx, in.ID, vendorID, u.ID, in.Body.Response); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listEscalatedComplaints(ctx context.Context, _ *struct{}) (*listComplaintsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	cs, err := a.Svc.ListEscalatedComplaints(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return complaintListResponse(cs), nil
}

func (a *API) adminResolveComplaint(ctx context.Context, in *adminResolveInput) (*struct{}, error) {
	u, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.AdminResolveComplaint(ctx, in.ID, u.ID, in.Body.Resolution, in.Body.Compensate); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func complaintListResponse(cs []*feedback.Complaint) *listComplaintsOutput {
	var resp listComplaintsOutput
	resp.Body.Items = make([]complaintDTO, 0, len(cs))
	for _, c := range cs {
		resp.Body.Items = append(resp.Body.Items, toComplaintDTO(c))
	}
	return &resp
}

// mapErr translates feedback sentinels to huma HTTP errors.
func mapErr(err error) error {
	switch {
	case errors.Is(err, feedback.ErrOrderNotFound),
		errors.Is(err, feedback.ErrComplaintNotFound),
		errors.Is(err, feedback.ErrRatingNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, feedback.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, feedback.ErrValidation):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, feedback.ErrOrderNotPickedUp),
		errors.Is(err, feedback.ErrAlreadyRated),
		errors.Is(err, feedback.ErrComplaintExists),
		errors.Is(err, feedback.ErrInvalidTransition),
		errors.Is(err, feedback.ErrEscalateTooEarly):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
