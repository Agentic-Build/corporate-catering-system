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

type plantMappingDTO struct {
	Plant         string `json:"plant"`
	ServiceWindow string `json:"service_window"`
}

type vendorDTO struct {
	ID            string            `json:"id"`
	DisplayName   string            `json:"display_name"`
	LegalName     string            `json:"legal_name"`
	ContactEmail  string            `json:"contact_email"`
	Status        string            `json:"status"`
	ApprovedAt    *string           `json:"approved_at,omitempty"`
	Plants        []string          `json:"plants,omitempty"`
	PlantMappings []plantMappingDTO `json:"plant_mappings,omitempty"`
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

type vendorSettingsDTO struct {
	CutoffHour         int `json:"cutoff_hour"`
	PreorderWindowDays int `json:"preorder_window_days"`
}

type merchantSettingsOutput struct {
	Body struct {
		Settings vendorSettingsDTO `json:"settings"`
	}
}

type updateMerchantSettingsInput struct {
	Body struct {
		CutoffHour         int `json:"cutoff_hour" minimum:"0" maximum:"23"`
		PreorderWindowDays int `json:"preorder_window_days" minimum:"1" maximum:"30"`
	}
}

type setPlantWindowInput struct {
	ID    string `path:"id" format:"uuid"`
	Plant string `path:"plant"`
	Body  struct {
		ServiceWindow string `json:"service_window" maxLength:"100" doc:"e.g. 11:30-13:00"`
	}
}

type operatorIDInput struct {
	ID         string `path:"id" format:"uuid"`
	OperatorID string `path:"operator_id" format:"uuid"`
}

type approveInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Plants []string `json:"plants"`
	}
}

type operatorDTO struct {
	ID              string  `json:"id"`
	VendorID        string  `json:"vendor_id"`
	Email           string  `json:"email"`
	DisplayName     string  `json:"display_name"`
	Provider        string  `json:"provider"`
	ExternalSubject *string `json:"external_subject,omitempty"`
	Status          string  `json:"status"`
	SetupURL        *string `json:"setup_url,omitempty"`
	LastSyncedAt    *string `json:"last_synced_at,omitempty"`
}

type listOperatorsOutput struct {
	Body struct {
		Items []operatorDTO `json:"items"`
	}
}

type createOperatorInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		Email       string `json:"email" format:"email"`
		DisplayName string `json:"display_name" minLength:"1"`
	}
}

type createOperatorOutput struct {
	Body struct {
		Operator operatorDTO `json:"operator"`
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
		OperationID: "listVendorOperators",
		Method:      http.MethodGet,
		Path:        "/api/admin/vendors/{id}/operators",
		Summary:     "List vendor operators",
		Tags:        []string{"admin", "vendor"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listOperators)

	huma.Register(api, huma.Operation{
		OperationID:   "createVendorOperator",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/operators",
		Summary:       "Create or update a vendor operator in Authentik",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.createOperator)

	huma.Register(api, huma.Operation{
		OperationID:   "suspendVendorOperator",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/operators/{operator_id}/suspend",
		Summary:       "Suspend a vendor operator",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.suspendOperator)

	huma.Register(api, huma.Operation{
		OperationID:   "reinstateVendorOperator",
		Method:        http.MethodPost,
		Path:          "/api/admin/vendors/{id}/operators/{operator_id}/reinstate",
		Summary:       "Reinstate a vendor operator",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.reinstateOperator)

	huma.Register(api, huma.Operation{
		OperationID:   "setVendorPlantWindow",
		Method:        http.MethodPut,
		Path:          "/api/admin/vendors/{id}/plants/{plant}/window",
		Summary:       "Set the service window for a vendor's plant mapping",
		Tags:          []string{"admin", "vendor"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.setPlantWindow)

	huma.Register(api, huma.Operation{
		OperationID: "getMerchantSettings",
		Method:      http.MethodGet,
		Path:        "/api/merchant/settings",
		Summary:     "Get own vendor's ordering settings",
		Tags:        []string{"merchant", "vendor"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.merchantGetSettings)

	huma.Register(api, huma.Operation{
		OperationID: "updateMerchantSettings",
		Method:      http.MethodPut,
		Path:        "/api/merchant/settings",
		Summary:     "Update own vendor's cutoff hour and preorder window",
		Tags:        []string{"merchant", "vendor"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.merchantUpdateSettings)
}

// requireVendor enforces a vendor_operator bound to a vendor, returning the
// vendor_id from the session.
func (a *API) requireVendor(ctx context.Context) (string, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return "", huma.Error403Forbidden("user is not bound to a vendor")
	}
	return *u.VendorID, nil
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

func (a *API) setPlantWindow(ctx context.Context, in *setPlantWindowInput) (*struct{}, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.Svc.SetPlantWindow(ctx, in.ID, in.Plant, in.Body.ServiceWindow); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) merchantGetSettings(ctx context.Context, _ *struct{}) (*merchantSettingsOutput, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := a.Svc.Get(ctx, vendorID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp merchantSettingsOutput
	resp.Body.Settings = vendorSettingsDTO{
		CutoffHour:         v.CutoffHour,
		PreorderWindowDays: v.PreorderWindowDays,
	}
	return &resp, nil
}

func (a *API) merchantUpdateSettings(ctx context.Context, in *updateMerchantSettingsInput) (*merchantSettingsOutput, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	v, err := a.Svc.UpdateSettings(ctx, vendorID, in.Body.CutoffHour, in.Body.PreorderWindowDays)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp merchantSettingsOutput
	resp.Body.Settings = vendorSettingsDTO{
		CutoffHour:         v.CutoffHour,
		PreorderWindowDays: v.PreorderWindowDays,
	}
	return &resp, nil
}

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
		mappings, _ := a.Svc.ListPlantMappings(ctx, v.ID)
		for _, m := range mappings {
			if !m.Active {
				continue
			}
			d.Plants = append(d.Plants, m.Plant)
			d.PlantMappings = append(d.PlantMappings, plantMappingDTO{
				Plant: m.Plant, ServiceWindow: m.ServiceWindow,
			})
		}
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

func (a *API) listOperators(ctx context.Context, in *vendorIDInput) (*listOperatorsOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	ops, err := a.Svc.ListOperators(ctx, in.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listOperatorsOutput
	resp.Body.Items = make([]operatorDTO, 0, len(ops))
	for _, op := range ops {
		resp.Body.Items = append(resp.Body.Items, toOperatorDTO(op))
	}
	return &resp, nil
}

func (a *API) createOperator(ctx context.Context, in *createOperatorInput) (*createOperatorOutput, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	op, err := a.Svc.CreateOperator(ctx, in.ID, in.Body.Email, in.Body.DisplayName)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp createOperatorOutput
	resp.Body.Operator = toOperatorDTO(op)
	return &resp, nil
}

func (a *API) suspendOperator(ctx context.Context, in *operatorIDInput) (*struct{}, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.Svc.SuspendOperator(ctx, in.ID, in.OperatorID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) reinstateOperator(ctx context.Context, in *operatorIDInput) (*struct{}, error) {
	if _, err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	if err := a.Svc.ReinstateOperator(ctx, in.ID, in.OperatorID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
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

func toOperatorDTO(op *vendor.OperatorAccount) operatorDTO {
	d := operatorDTO{
		ID:              op.ID,
		VendorID:        op.VendorID,
		Email:           op.Email,
		DisplayName:     op.DisplayName,
		Provider:        op.Provider,
		ExternalSubject: op.ExternalSubject,
		Status:          string(op.Status),
		SetupURL:        op.SetupURL,
	}
	if op.LastSyncedAt != nil {
		s := op.LastSyncedAt.Format("2006-01-02T15:04:05Z")
		d.LastSyncedAt = &s
	}
	return d
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, vendor.ErrVendorNotFound), errors.Is(err, vendor.ErrOperatorNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, vendor.ErrAlreadyApproved), errors.Is(err, vendor.ErrInvalidStatus):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, vendor.ErrInvalidOperator), errors.Is(err, vendor.ErrInvalidSettings):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, vendor.ErrProvisioningSetup):
		return huma.NewError(http.StatusBadGateway, err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
