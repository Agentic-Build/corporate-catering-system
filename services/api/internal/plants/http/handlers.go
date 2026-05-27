package phttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/plants"
	vendor "github.com/takalawang/corporate-catering-system/services/api/internal/vendors"
)

// API exposes plant registry endpoints.
type API struct {
	Svc       *plants.Service
	VendorSvc *vendor.Service
}

// ----- DTOs -----

type plantDTO struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	Address   string `json:"address"`
	Active    bool   `json:"active"`
	SortOrder int    `json:"sort_order"`
}

type listPlantsOutput struct {
	Body struct {
		Items []plantDTO `json:"items"`
	}
}

type createPlantInput struct {
	Body struct {
		Code      string `json:"code" minLength:"1"`
		Label     string `json:"label" minLength:"1"`
		Address   string `json:"address"`
		SortOrder int    `json:"sort_order"`
	}
}

type createPlantOutput struct {
	Body struct {
		Plant plantDTO `json:"plant"`
	}
}

type updatePlantInput struct {
	Code string `path:"code"`
	Body struct {
		Label     string `json:"label" minLength:"1"`
		Address   string `json:"address"`
		Active    bool   `json:"active"`
		SortOrder int    `json:"sort_order"`
	}
}

type updatePlantOutput struct {
	Body struct {
		Plant plantDTO `json:"plant"`
	}
}

type setMerchantPlantsInput struct {
	Body struct {
		Plants []string `json:"plants"`
	}
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	// Public (active only) — any authenticated user.
	huma.Register(api, huma.Operation{
		OperationID: "listPlants",
		Method:      http.MethodGet,
		Path:        "/api/plants",
		Summary:     "List active plants",
		Tags:        []string{"plant"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listActive)

	// Admin — welfare_admin only.
	huma.Register(api, huma.Operation{
		OperationID: "listPlantsAdmin",
		Method:      http.MethodGet,
		Path:        "/api/admin/plants",
		Summary:     "List all plants (admin)",
		Tags:        []string{"admin", "plant"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listAll)

	huma.Register(api, huma.Operation{
		OperationID:   "createPlant",
		Method:        http.MethodPost,
		Path:          "/api/admin/plants",
		Summary:       "Create a plant",
		Tags:          []string{"admin", "plant"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.create)

	huma.Register(api, huma.Operation{
		OperationID: "updatePlant",
		Method:      http.MethodPut,
		Path:        "/api/admin/plants/{code}",
		Summary:     "Update a plant",
		Tags:        []string{"admin", "plant"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.update)

	// Merchant — vendor_operator only.
	huma.Register(api, huma.Operation{
		OperationID: "getMerchantPlants",
		Method:      http.MethodGet,
		Path:        "/api/merchant/plants",
		Summary:     "Get own vendor's plant mappings",
		Tags:        []string{"merchant", "plant"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.merchantList)

	huma.Register(api, huma.Operation{
		OperationID:   "setMerchantPlants",
		Method:        http.MethodPut,
		Path:          "/api/merchant/plants",
		Summary:       "Set own vendor's plant assignments",
		Tags:          []string{"merchant", "plant"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.merchantSet)
}

// ----- Handlers -----

func (a *API) listActive(ctx context.Context, _ *struct{}) (*listPlantsOutput, error) {
	if _, ok := idhttp.UserFromContext(ctx); !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	list, err := a.Svc.ListActive(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("internal", err)
	}
	var resp listPlantsOutput
	resp.Body.Items = toDTOs(list)
	return &resp, nil
}

func (a *API) listAll(ctx context.Context, _ *struct{}) (*listPlantsOutput, error) {
	if err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	list, err := a.Svc.ListAll(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("internal", err)
	}
	var resp listPlantsOutput
	resp.Body.Items = toDTOs(list)
	return &resp, nil
}

func (a *API) create(ctx context.Context, in *createPlantInput) (*createPlantOutput, error) {
	if err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := a.Svc.Create(ctx, in.Body.Code, in.Body.Label, in.Body.Address, in.Body.SortOrder)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp createPlantOutput
	resp.Body.Plant = toDTO(p)
	return &resp, nil
}

func (a *API) update(ctx context.Context, in *updatePlantInput) (*updatePlantOutput, error) {
	if err := a.requireAdmin(ctx); err != nil {
		return nil, err
	}
	p, err := a.Svc.Update(ctx, in.Code, in.Body.Label, in.Body.Address, in.Body.Active, in.Body.SortOrder)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp updatePlantOutput
	resp.Body.Plant = toDTO(p)
	return &resp, nil
}

func (a *API) merchantList(ctx context.Context, _ *struct{}) (*listPlantsOutput, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	mappings, err := a.VendorSvc.ListPlantMappings(ctx, vendorID)
	if err != nil {
		return nil, huma.Error500InternalServerError("internal", err)
	}
	var resp listPlantsOutput
	resp.Body.Items = make([]plantDTO, 0, len(mappings))
	for _, m := range mappings {
		if !m.Active {
			continue
		}
		// Try to enrich with registry label/address; fall back to code.
		dto := plantDTO{Code: m.Plant, Label: m.Plant}
		if p, err := a.Svc.Get(ctx, m.Plant); err == nil {
			dto.Label = p.Label
			dto.Address = p.Address
			dto.Active = p.Active
			dto.SortOrder = p.SortOrder
		}
		resp.Body.Items = append(resp.Body.Items, dto)
	}
	return &resp, nil
}

func (a *API) merchantSet(ctx context.Context, in *setMerchantPlantsInput) (*struct{}, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.ValidateActiveCodes(ctx, in.Body.Plants); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if err := a.VendorSvc.Plants.Set(ctx, vendorID, in.Body.Plants); err != nil {
		return nil, huma.Error500InternalServerError("internal", err)
	}
	return &struct{}{}, nil
}

// ----- Auth guards -----

func (a *API) requireAdmin(ctx context.Context) error {
	_, err := idhttp.RequireAdmin(ctx)
	return err
}

func (a *API) requireVendor(ctx context.Context) (string, error) {
	_, vendorID, err := idhttp.RequireVendor(ctx)
	return vendorID, err
}

// ----- Helpers -----

func toDTO(p *plants.Plant) plantDTO {
	return plantDTO{
		Code:      p.Code,
		Label:     p.Label,
		Address:   p.Address,
		Active:    p.Active,
		SortOrder: p.SortOrder,
	}
}

func toDTOs(list []*plants.Plant) []plantDTO {
	out := make([]plantDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toDTO(p))
	}
	return out
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, plants.ErrInvalid):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, plants.ErrPlantNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, plants.ErrDuplicateCode):
		return huma.Error409Conflict(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
