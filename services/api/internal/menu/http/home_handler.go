package mhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// HomeAPI exposes the employee landing-page aggregate endpoint plus the two
// "see more" pagination endpoints for reorder + recommendation chips. The
// favorites pagination endpoint is owned by the existing menu API (Task 2).
//
// Constructor wiring (in cmd/tbite/main.go):
//
//	homeAPI := &mhttp.HomeAPI{
//	    Home:    homeService,
//	    MenuSvc: menuService, // for day_menu
//	}
//	homeAPI.Register(api)
type HomeAPI struct {
	// Home is the personalisation orchestrator (target_day + chips).
	Home *menu.HomeService
	// MenuSvc is the existing menu service used to fetch the day_menu list.
	// Reused as-is so the home page returns the same shape that
	// GET /api/employee/menu does for "today".
	MenuSvc *menu.Service
}

// ---------- DTOs ----------

type orderSummaryDTO struct {
	OrderID         string `json:"order_id"`
	VendorID        string `json:"vendor_id"`
	Status          string `json:"status"`
	TotalPriceMinor int64  `json:"total_price_minor"`
	CutoffAt        string `json:"cutoff_at"`
}

type reorderChipDTO struct {
	SourceOrderID   string   `json:"source_order_id"`
	VendorID        string   `json:"vendor_id"`
	VendorName      string   `json:"vendor_name"`
	ItemsPreview    []string `json:"items_preview"`
	TotalPriceMinor int64    `json:"total_price_minor"`
	Freq            int      `json:"freq"`
	AvailableToday  bool     `json:"available_today"`
}

// favoriteChipDTO is reused from favorites_handler.go (same package). Keeping
// a single shape ensures /home, /favorites, and the see-more endpoint all
// emit identical JSON for a favorite chip.

type recommendChipDTO struct {
	MenuItemID string  `json:"menu_item_id"`
	Name       string  `json:"name"`
	UnitPrice  int64   `json:"unit_price"`
	VendorID   string  `json:"vendor_id"`
	Score      float64 `json:"score"`
	Reason     string  `json:"reason"`
}

type homeOutput struct {
	Body struct {
		TargetDay      string                `json:"target_day"`
		HasOrdered     bool                  `json:"has_ordered"`
		OrderSummary   *orderSummaryDTO      `json:"order_summary,omitempty"`
		ReorderChips   []reorderChipDTO      `json:"reorder_chips"`
		FavoriteChips  []favoriteChipDTO     `json:"favorite_chips"`
		RecommendChips []recommendChipDTO    `json:"recommend_chips"`
		DayMenu        []employeeMenuItemDTO `json:"day_menu"`
	}
}

type homeInput struct {
	Day string `query:"day" doc:"YYYY-MM-DD; overrides server-derived target_day"`
}

type reordersInput struct {
	Cursor int `query:"cursor" doc:"opaque integer offset returned by a previous response (0 to start)"`
	Limit  int `query:"limit" minimum:"1" maximum:"50" doc:"page size; defaults to 5"`
}

type reordersOutput struct {
	Body struct {
		Chips      []reorderChipDTO `json:"chips"`
		NextCursor *int             `json:"next_cursor,omitempty"`
	}
}

type recommendationsInput struct {
	Day    string `query:"day" doc:"YYYY-MM-DD target day; defaults to server-derived target_day"`
	Cursor int    `query:"cursor" doc:"opaque integer offset (0 to start)"`
	Limit  int    `query:"limit" minimum:"1" maximum:"50" doc:"page size; defaults to 5"`
}

type recommendationsOutput struct {
	Body struct {
		Chips      []recommendChipDTO `json:"chips"`
		NextCursor *int               `json:"next_cursor,omitempty"`
	}
}

// ---------- Registration ----------

func (a *HomeAPI) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getEmployeeHome",
		Method:      http.MethodGet,
		Path:        "/api/employee/home",
		Summary:     "Aggregate landing page (target_day + 3 chips + day_menu)",
		Tags:        []string{"employee", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.getHome)

	huma.Register(api, huma.Operation{
		OperationID: "listEmployeeReorders",
		Method:      http.MethodGet,
		Path:        "/api/employee/reorders",
		Summary:     "Paginated reorder chips (再點一次)",
		Tags:        []string{"employee", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listReorders)

	huma.Register(api, huma.Operation{
		OperationID: "listEmployeeRecommendations",
		Method:      http.MethodGet,
		Path:        "/api/employee/recommendations",
		Summary:     "Paginated recommendation chips (推薦你今天)",
		Tags:        []string{"employee", "menu"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listRecommendations)
}

// ---------- Handlers ----------

func (a *HomeAPI) getHome(ctx context.Context, in *homeInput) (*homeOutput, error) {
	user, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	plant, err := requireEmployeePlant(user)
	if err != nil {
		return nil, err
	}

	state, err := a.Home.Compute(ctx, user.ID, plant, in.Day)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	dayT, err := time.Parse("2006-01-02", state.TargetDay)
	if err != nil {
		return nil, huma.Error500InternalServerError("internal: bad target_day", err)
	}

	reorders, _, err := a.Home.ReorderChips(ctx, user.ID, dayT, 0, 5)
	if err != nil {
		return nil, huma.Error500InternalServerError("reorder chips", err)
	}
	favorites, _, err := a.Home.FavoriteChipsList(ctx, user.ID, state.TargetDay, plant, 5, nil)
	if err != nil {
		return nil, huma.Error500InternalServerError("favorite chips", err)
	}
	recs, _, err := a.Home.RecommendChips(ctx, user.ID, plant, dayT, 0, 5)
	if err != nil {
		return nil, huma.Error500InternalServerError("recommendation chips", err)
	}
	dayMenu, err := a.MenuSvc.ListForEmployee(ctx, menu.EmployeeMenuFilter{Plant: plant, Day: dayT})
	if err != nil {
		return nil, mapErr(err)
	}

	var resp homeOutput
	resp.Body.TargetDay = state.TargetDay
	resp.Body.HasOrdered = state.HasOrdered
	if state.OrderSummary != nil {
		resp.Body.OrderSummary = &orderSummaryDTO{
			OrderID:         state.OrderSummary.OrderID,
			VendorID:        state.OrderSummary.VendorID,
			Status:          state.OrderSummary.Status,
			TotalPriceMinor: state.OrderSummary.TotalPriceMinor,
			CutoffAt:        state.OrderSummary.CutoffAt.UTC().Format(time.RFC3339),
		}
	}
	resp.Body.ReorderChips = reorderChipsToDTO(reorders)
	resp.Body.FavoriteChips = favoriteChipsToDTO(favorites)
	resp.Body.RecommendChips = recommendChipsToDTO(recs)
	resp.Body.DayMenu = make([]employeeMenuItemDTO, 0, len(dayMenu))
	for _, it := range dayMenu {
		resp.Body.DayMenu = append(resp.Body.DayMenu, employeeMenuItemDTO{
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

func (a *HomeAPI) listReorders(ctx context.Context, in *reordersInput) (*reordersOutput, error) {
	user, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	plant, err := requireEmployeePlant(user)
	if err != nil {
		return nil, err
	}
	// We need a target day to compute available_today; reuse Compute() with no
	// day override so the cursor-based paging stays consistent with /home.
	state, err := a.Home.Compute(ctx, user.ID, plant, "")
	if err != nil {
		return nil, huma.Error500InternalServerError("compute target day", err)
	}
	dayT, _ := time.Parse("2006-01-02", state.TargetDay)
	limit := in.Limit
	if limit == 0 {
		limit = 5
	}
	chips, next, err := a.Home.ReorderChips(ctx, user.ID, dayT, in.Cursor, limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("reorder chips", err)
	}
	var resp reordersOutput
	resp.Body.Chips = reorderChipsToDTO(chips)
	if next >= 0 {
		resp.Body.NextCursor = &next
	}
	return &resp, nil
}

func (a *HomeAPI) listRecommendations(ctx context.Context, in *recommendationsInput) (*recommendationsOutput, error) {
	user, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	plant, err := requireEmployeePlant(user)
	if err != nil {
		return nil, err
	}
	dayStr := in.Day
	if dayStr == "" {
		state, derr := a.Home.Compute(ctx, user.ID, plant, "")
		if derr != nil {
			return nil, huma.Error500InternalServerError("compute target day", derr)
		}
		dayStr = state.TargetDay
	}
	dayT, perr := time.Parse("2006-01-02", dayStr)
	if perr != nil {
		return nil, huma.Error400BadRequest("day must be YYYY-MM-DD")
	}
	limit := in.Limit
	if limit == 0 {
		limit = 5
	}
	chips, next, err := a.Home.RecommendChips(ctx, user.ID, plant, dayT, in.Cursor, limit)
	if err != nil {
		return nil, huma.Error500InternalServerError("recommendation chips", err)
	}
	var resp recommendationsOutput
	resp.Body.Chips = recommendChipsToDTO(chips)
	if next >= 0 {
		resp.Body.NextCursor = &next
	}
	return &resp, nil
}

// ---------- Helpers ----------

// requireEmployeePlant returns the user's plant string or a 4xx huma error.
// Role enforcement is delegated to upstream auth middleware — the home
// endpoints are mounted at /api/employee/* which already requires an
// authenticated employee.
func requireEmployeePlant(u *identity.User) (string, error) {
	if u.Plant == nil || *u.Plant == "" {
		return "", huma.Error400BadRequest("plant is required (user has no plant assignment)")
	}
	return *u.Plant, nil
}

func reorderChipsToDTO(in []menu.ReorderChip) []reorderChipDTO {
	out := make([]reorderChipDTO, 0, len(in))
	for _, c := range in {
		preview := c.ItemsPreview
		if preview == nil {
			preview = []string{}
		}
		out = append(out, reorderChipDTO{
			SourceOrderID:   c.SourceOrderID,
			VendorID:        c.VendorID,
			VendorName:      c.VendorName,
			ItemsPreview:    preview,
			TotalPriceMinor: c.TotalPriceMinor,
			Freq:            c.Freq,
			AvailableToday:  c.AvailableToday,
		})
	}
	return out
}

func favoriteChipsToDTO(in []menu.FavoriteChip) []favoriteChipDTO {
	out := make([]favoriteChipDTO, 0, len(in))
	for _, c := range in {
		out = append(out, favoriteChipDTO{
			MenuItemID:     c.MenuItemID,
			Name:           c.Name,
			UnitPrice:      c.UnitPrice,
			VendorID:       c.VendorID,
			AvailableToday: c.AvailableToday,
		})
	}
	return out
}

func recommendChipsToDTO(in []menu.RecommendChip) []recommendChipDTO {
	out := make([]recommendChipDTO, 0, len(in))
	for _, c := range in {
		out = append(out, recommendChipDTO{
			MenuItemID: c.MenuItemID,
			Name:       c.Name,
			UnitPrice:  c.UnitPrice,
			VendorID:   c.VendorID,
			Score:      c.Score,
			Reason:     c.Reason,
		})
	}
	return out
}
