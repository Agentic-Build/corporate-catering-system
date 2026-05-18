package mhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// API exposes merchant CRUD + employee read endpoints for the menu domain.
// Merchant routes require a vendor_operator bound to a vendor (user.VendorID);
// the employee read route requires an employee with a plant assignment.
type API struct {
	Svc *menu.Service
}

// ----- DTOs -----

type categoryDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

type itemDTO struct {
	ID          string   `json:"id"`
	VendorID    string   `json:"vendor_id"`
	CategoryID  *string  `json:"category_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceMinor  int64    `json:"price_minor"`
	Tags        []string `json:"tags"`
	Badges      []string `json:"badges"`
	Status      string   `json:"status"`
	Images      []string `json:"images,omitempty"`
}

// merchantItemDTO is the item projection for GET /api/merchant/menu-items.
// It is itemDTO plus two read-only usage stats the meal-library view needs.
type merchantItemDTO struct {
	ID          string   `json:"id"`
	VendorID    string   `json:"vendor_id"`
	CategoryID  *string  `json:"category_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	PriceMinor  int64    `json:"price_minor"`
	Tags        []string `json:"tags"`
	Badges      []string `json:"badges"`
	Status      string   `json:"status"`
	Images      []string `json:"images,omitempty"`
	LastUsed    *string  `json:"last_used" doc:"Most recent supply date (YYYY-MM-DD), null if never scheduled"`
	TotalSold   int      `json:"total_sold" doc:"Cumulative quantity sold across picked-up orders"`
}

type employeeMenuItemDTO struct {
	ID           string   `json:"id"`
	Vendor       string   `json:"vendor"`
	VendorID     string   `json:"vendor_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	PriceMinor   int64    `json:"price_minor"`
	Tags         []string `json:"tags"`
	Badges       []string `json:"badges"`
	Images       []string `json:"images,omitempty"`
	Remain       int      `json:"remain"`
	Capacity     int      `json:"capacity"`
	PickupWindow string   `json:"pickup_window"`
	ETALabel     string   `json:"eta_label"`
	SoldOut      bool     `json:"sold_out"`
}

// ----- Inputs / Outputs -----

type listCategoriesOutput struct {
	Body struct {
		Items []categoryDTO `json:"items"`
	}
}

type createCategoryInput struct {
	Body struct {
		Name      string `json:"name" minLength:"1"`
		SortOrder int    `json:"sort_order"`
	}
}

type createCategoryOutput struct {
	Body struct {
		Category categoryDTO `json:"category"`
	}
}

type createItemInput struct {
	Body struct {
		CategoryID  *string  `json:"category_id,omitempty"`
		Name        string   `json:"name" minLength:"1"`
		Description string   `json:"description"`
		PriceMinor  int64    `json:"price_minor" minimum:"0"`
		Tags        []string `json:"tags"`
		Badges      []string `json:"badges"`
	}
}

type itemOutput struct {
	Body struct {
		Item itemDTO `json:"item"`
	}
}

type updateItemInput struct {
	ID   string `path:"id" format:"uuid"`
	Body struct {
		CategoryID  *string  `json:"category_id,omitempty"`
		Name        string   `json:"name" minLength:"1"`
		Description string   `json:"description"`
		PriceMinor  int64    `json:"price_minor" minimum:"0"`
		Tags        []string `json:"tags"`
		Badges      []string `json:"badges"`
	}
}

type itemIDInput struct {
	ID string `path:"id" format:"uuid"`
}

type listItemsInput struct {
	IncludeArchived bool `query:"include_archived" doc:"Include archived items in the result"`
}

type listItemsOutput struct {
	Body struct {
		Items []merchantItemDTO `json:"items"`
	}
}

type listEmployeeMenuInput struct {
	Plant    string   `query:"plant" doc:"Plant code; defaults to caller's plant"`
	Day      string   `query:"day" doc:"YYYY-MM-DD; defaults to today UTC"`
	Q        string   `query:"q" doc:"Keyword matched against item name/description"`
	Tags     []string `query:"tags" doc:"Health tags; an item matches if it carries ANY of these"`
	PriceMin int64    `query:"price_min" minimum:"0" doc:"Inclusive minimum price in minor units; 0 = no lower bound"`
	PriceMax int64    `query:"price_max" minimum:"0" doc:"Inclusive maximum price in minor units; 0 = no upper bound"`
	InStock  bool     `query:"in_stock" doc:"When true, exclude sold-out items"`
	Sort     string   `query:"sort" enum:"name,price_asc,price_desc,remain" doc:"Result ordering; defaults to vendor then name"`
}

type listEmployeeMenuOutput struct {
	Body struct {
		Items []employeeMenuItemDTO `json:"items"`
	}
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listMerchantCategories",
		Method:      http.MethodGet,
		Path:        "/api/merchant/categories",
		Summary:     "List the merchant's menu categories",
		Tags:        []string{"merchant", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listCategories)

	huma.Register(api, huma.Operation{
		OperationID:   "createMerchantCategory",
		Method:        http.MethodPost,
		Path:          "/api/merchant/categories",
		Summary:       "Create a menu category for the merchant",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.createCategory)

	huma.Register(api, huma.Operation{
		OperationID: "listMerchantMenuItems",
		Method:      http.MethodGet,
		Path:        "/api/merchant/menu-items",
		Summary:     "List the merchant's menu items",
		Tags:        []string{"merchant", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listItems)

	huma.Register(api, huma.Operation{
		OperationID:   "createMerchantMenuItem",
		Method:        http.MethodPost,
		Path:          "/api/merchant/menu-items",
		Summary:       "Create a draft menu item",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.createItem)

	huma.Register(api, huma.Operation{
		OperationID: "updateMerchantMenuItem",
		Method:      http.MethodPatch,
		Path:        "/api/merchant/menu-items/{id}",
		Summary:     "Update a menu item",
		Tags:        []string{"merchant", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.updateItem)

	huma.Register(api, huma.Operation{
		OperationID:   "publishMerchantMenuItem",
		Method:        http.MethodPost,
		Path:          "/api/merchant/menu-items/{id}/publish",
		Summary:       "Publish a draft/archived menu item (status=active)",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.publishItem)

	huma.Register(api, huma.Operation{
		OperationID:   "archiveMerchantMenuItem",
		Method:        http.MethodPost,
		Path:          "/api/merchant/menu-items/{id}/archive",
		Summary:       "Archive a menu item",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.archiveItem)

	huma.Register(api, huma.Operation{
		OperationID:   "copyMerchantMenuItem",
		Method:        http.MethodPost,
		Path:          "/api/merchant/menu-items/{id}/copy",
		Summary:       "Duplicate a menu item into a fresh draft",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.copyItem)

	huma.Register(api, huma.Operation{
		OperationID: "listEmployeeMenu",
		Method:      http.MethodGet,
		Path:        "/api/employee/menu",
		Summary:     "List the employee's available menu for a plant + day",
		Tags:        []string{"employee", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listEmployeeMenu)
}

// ----- Auth helpers -----

// requireVendor enforces vendor_operator role + a non-empty vendor binding.
// Returns (user, vendorID, error).
func (a *API) requireVendor(ctx context.Context) (*identity.User, string, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return nil, "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return nil, "", huma.Error403Forbidden("user is not bound to a vendor")
	}
	return u, *u.VendorID, nil
}

// requireEmployee enforces employee role.
func (a *API) requireEmployee(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleEmployee {
		return nil, huma.Error403Forbidden("employee role required")
	}
	return u, nil
}

// ----- Handlers -----

func (a *API) listCategories(ctx context.Context, _ *struct{}) (*listCategoriesOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	cats, err := a.Svc.ListCategories(ctx, vendorID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listCategoriesOutput
	resp.Body.Items = make([]categoryDTO, 0, len(cats))
	for _, c := range cats {
		resp.Body.Items = append(resp.Body.Items, categoryDTO{ID: c.ID, Name: c.Name, SortOrder: c.SortOrder})
	}
	return &resp, nil
}

func (a *API) createCategory(ctx context.Context, in *createCategoryInput) (*createCategoryOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := a.Svc.CreateCategory(ctx, menu.CreateCategoryInput{
		VendorID:  vendorID,
		Name:      in.Body.Name,
		SortOrder: in.Body.SortOrder,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp createCategoryOutput
	resp.Body.Category = categoryDTO{ID: c.ID, Name: c.Name, SortOrder: c.SortOrder}
	return &resp, nil
}

func (a *API) listItems(ctx context.Context, in *listItemsInput) (*listItemsOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := a.Svc.ListByVendor(ctx, vendorID, in.IncludeArchived)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listItemsOutput
	resp.Body.Items = make([]merchantItemDTO, 0, len(rows))
	for _, row := range rows {
		resp.Body.Items = append(resp.Body.Items, toMerchantItemDTO(row))
	}
	return &resp, nil
}

func (a *API) createItem(ctx context.Context, in *createItemInput) (*itemOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	it, err := a.Svc.CreateItem(ctx, menu.CreateItemInput{
		VendorID:    vendorID,
		CategoryID:  in.Body.CategoryID,
		Name:        in.Body.Name,
		Description: in.Body.Description,
		PriceMinor:  in.Body.PriceMinor,
		Tags:        in.Body.Tags,
		Badges:      in.Body.Badges,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp itemOutput
	resp.Body.Item = toItemDTO(it)
	return &resp, nil
}

func (a *API) updateItem(ctx context.Context, in *updateItemInput) (*itemOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	it, err := a.Svc.UpdateItem(ctx, in.ID, vendorID, menu.UpdateItemInput{
		Name:        in.Body.Name,
		Description: in.Body.Description,
		PriceMinor:  in.Body.PriceMinor,
		Tags:        in.Body.Tags,
		Badges:      in.Body.Badges,
		CategoryID:  in.Body.CategoryID,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp itemOutput
	resp.Body.Item = toItemDTO(it)
	return &resp, nil
}

func (a *API) copyItem(ctx context.Context, in *itemIDInput) (*itemOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	it, err := a.Svc.CopyItem(ctx, in.ID, vendorID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp itemOutput
	resp.Body.Item = toItemDTO(it)
	return &resp, nil
}

func (a *API) publishItem(ctx context.Context, in *itemIDInput) (*struct{}, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Publish(ctx, in.ID, vendorID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) archiveItem(ctx context.Context, in *itemIDInput) (*struct{}, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Archive(ctx, in.ID, vendorID); err != nil {
		return nil, mapErr(err)
	}
	return &struct{}{}, nil
}

func (a *API) listEmployeeMenu(ctx context.Context, in *listEmployeeMenuInput) (*listEmployeeMenuOutput, error) {
	user, err := a.requireEmployee(ctx)
	if err != nil {
		return nil, err
	}
	// Resolve plant: caller plant takes precedence; fall back to ?plant=.
	plant := ""
	if user.Plant != nil {
		plant = *user.Plant
	}
	if plant == "" {
		plant = in.Plant
	}
	if plant == "" {
		return nil, huma.Error400BadRequest("plant is required (user has no plant assignment)")
	}
	// Resolve day: empty => today UTC at midnight; otherwise parse YYYY-MM-DD.
	var day time.Time
	if in.Day == "" {
		day = time.Now().UTC().Truncate(24 * time.Hour)
	} else {
		d, perr := time.Parse("2006-01-02", in.Day)
		if perr != nil {
			return nil, huma.Error400BadRequest("day must be YYYY-MM-DD")
		}
		day = d.UTC()
	}
	// huma forbids pointer query params, so the wire struct uses value types;
	// 0 / false means "not supplied" and maps to a nil filter field (no filter).
	filter := menu.EmployeeMenuFilter{
		Plant: plant,
		Day:   day,
		Q:     in.Q,
		Tags:  in.Tags,
		Sort:  menu.EmployeeMenuSort(in.Sort),
	}
	if in.PriceMin > 0 {
		filter.PriceMin = &in.PriceMin
	}
	if in.PriceMax > 0 {
		filter.PriceMax = &in.PriceMax
	}
	if in.InStock {
		inStock := true
		filter.InStock = &inStock
	}
	items, err := a.Svc.ListForEmployee(ctx, filter)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp listEmployeeMenuOutput
	resp.Body.Items = make([]employeeMenuItemDTO, 0, len(items))
	for _, it := range items {
		resp.Body.Items = append(resp.Body.Items, employeeMenuItemDTO{
			ID:           it.ID,
			Vendor:       it.VendorName,
			VendorID:     it.VendorID,
			Name:         it.Name,
			Description:  it.Description,
			PriceMinor:   it.PriceMinor,
			Tags:         it.Tags,
			Badges:       it.Badges,
			Images:       it.Images,
			Remain:       it.Remain,
			Capacity:     it.Capacity,
			PickupWindow: it.PickupWindow,
			ETALabel:     it.ETALabel,
			SoldOut:      it.SoldOut,
		})
	}
	return &resp, nil
}

// ----- helpers -----

func toItemDTO(i *menu.Item) itemDTO {
	tags := i.Tags
	if tags == nil {
		tags = []string{}
	}
	badges := i.Badges
	if badges == nil {
		badges = []string{}
	}
	return itemDTO{
		ID:          i.ID,
		VendorID:    i.VendorID,
		CategoryID:  i.CategoryID,
		Name:        i.Name,
		Description: i.Description,
		PriceMinor:  i.PriceMinor,
		Tags:        tags,
		Badges:      badges,
		Status:      string(i.Status),
	}
}

// toMerchantItemDTO maps a repo MerchantItemRow to the merchant list DTO,
// formatting last_used as a YYYY-MM-DD date string (null when never scheduled).
func toMerchantItemDTO(row *menu.MerchantItemRow) merchantItemDTO {
	d := toItemDTO(&row.Item)
	out := merchantItemDTO{
		ID:          d.ID,
		VendorID:    d.VendorID,
		CategoryID:  d.CategoryID,
		Name:        d.Name,
		Description: d.Description,
		PriceMinor:  d.PriceMinor,
		Tags:        d.Tags,
		Badges:      d.Badges,
		Status:      d.Status,
		Images:      d.Images,
		TotalSold:   row.TotalSold,
	}
	if row.LastUsed != nil {
		s := row.LastUsed.Format("2006-01-02")
		out.LastUsed = &s
	}
	return out
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, menu.ErrItemNotFound),
		errors.Is(err, menu.ErrCategoryNotFound),
		errors.Is(err, menu.ErrImageNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, menu.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
