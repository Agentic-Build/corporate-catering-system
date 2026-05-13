package vhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// API exposes admin vendor management endpoints. All routes require a
// welfare_admin user (enforced by requireAdmin).
type API struct {
	Svc *vendor.Service
}

// ----- DTOs -----

type vendorDTO struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	LegalName    string   `json:"legal_name"`
	ContactEmail string   `json:"contact_email"`
	Status       string   `json:"status"`
	ApprovedAt   *string  `json:"approved_at,omitempty"`
	Plants       []string `json:"plants,omitempty"`
}

type listVendorsInput struct {
	Status string `query:"status" enum:"pending,approved,suspended,terminated," doc:"Optional status filter"`
}

type listVendorsOutput struct {
	Body struct {
		Items []vendorDTO `json:"items"`
	}
}

type createVendorInput struct {
	Body struct {
		DisplayName  string `json:"display_name" minLength:"1"`
		LegalName    string `json:"legal_name" minLength:"1"`
		ContactEmail string `json:"contact_email" format:"email"`
	}
}

type createVendorOutput struct {
	Body struct {
		Vendor vendorDTO `json:"vendor"`
	}
}

type vendorIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type approveInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Plants []string `json:"plants"`
	}
}

type inviteOutput struct {
	Body struct {
		Code string `json:"code"`
	}
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listVendors",
		Method:      http.MethodGet,
		Path:        "/api/admin/vendors",
		Summary:     "List vendors (admin)",
		Tags:        []string{"admin", "vendor"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.list)

	huma.Register(api, huma.Operation{
		OperationID:   "createPendingVendor",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors",
		Summary:       "Create a pending vendor",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.create)

	huma.Register(api, huma.Operation{
		OperationID:   "approveVendor",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/approve",
		Summary:       "Approve vendor + set plants",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.approve)

	huma.Register(api, huma.Operation{
		OperationID:   "suspendVendor",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/suspend",
		Summary:       "Suspend an approved vendor",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.suspend)

	huma.Register(api, huma.Operation{
		OperationID:   "reinstateVendor",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/reinstate",
		Summary:       "Reinstate a suspended vendor",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.reinstate)

	huma.Register(api, huma.Operation{
		OperationID:   "issueVendorInvite",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/invite",
		Summary:       "Issue a single-use invite code for a vendor",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.invite)
}

// ----- Auth guard -----

func (a *API) requireAdmin(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleWelfareAdmin {
		return nil, huma.Error403Forbidden("admin role required")
	}
	return u, nil
}

// ----- Handlers -----

func (a *API) list(ctx context.Context, in *listVendorsInput) (*listVendorsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	var statuses []vendor.Status
	if in.Status != "" {
		statuses = []vendor.Status{vendor.Status(in.Status)}
	}
	vs, err := a.Svc.List(ctx, statuses)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listVendorsOutput
	resp.Body.Items = make([]vendorDTO, 0, len(vs))
	for _, v := range vs {
		d := toDTO(v)
		plants, _ := a.Svc.ListPlants(ctx, v.ID)
		d.Plants = plants
		resp.Body.Items = append(resp.Body.Items, d)
	}
	return &resp, nil
}

func (a *API) create(ctx context.Context, in *createVendorInput) (*createVendorOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	v, err := a.Svc.CreatePending(ctx, in.Body.DisplayName, in.Body.LegalName, in.Body.ContactEmail)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp createVendorOutput
	resp.Body.Vendor = toDTO(v)
	return &resp, nil
}

func (a *API) approve(ctx context.Context, in *approveInput) (*struct{}, error) {
	user, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Approve(ctx, in.ID, user.ID, in.Body.Plants); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) suspend(ctx context.Context, in *vendorIDInput) (*struct{}, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.Svc.Suspend(ctx, in.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) reinstate(ctx context.Context, in *vendorIDInput) (*struct{}, error) {
	user, err := a.requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Reinstate(ctx, in.ID, user.ID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) invite(ctx context.Context, in *vendorIDInput) (*inviteOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	code, err := a.Svc.IssueInvite(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp inviteOutput
	resp.Body.Code = code
	return &resp, nil
}

// ----- Helpers -----

func toDTO(v *vendor.Vendor) vendorDTO {
	d := vendorDTO{
		ID:           v.ID,
		DisplayName:  v.DisplayName,
		LegalName:    v.LegalName,
		ContactEmail: v.ContactEmail,
		Status:       string(v.Status),
	}
	if v.ApprovedAt != nil {
		s := v.ApprovedAt.Format("2006-01-02T15:04:05Z")
		d.ApprovedAt = &s
	}
	return d
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, vendor.ErrVendorNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, vendor.ErrAlreadyApproved), errors.Is(err, vendor.ErrInvalidStatus):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
