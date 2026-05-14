package mhttp

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// FavoritesAPI registers the employee-facing favorites endpoints. It is
// deliberately separate from API so the main menu CRUD surface remains
// untouched while P9 is rolled out.
type FavoritesAPI struct {
	Svc *menu.FavoritesService
}

// ----- DTOs -----

type favoriteChipDTO struct {
	MenuItemID     string `json:"menu_item_id"`
	Name           string `json:"name"`
	UnitPrice      int64  `json:"unit_price"`
	VendorID       string `json:"vendor_id"`
	AvailableToday bool   `json:"available_today"`
}

// ----- Inputs / Outputs -----

type addFavoriteInput struct {
	Body struct {
		MenuItemID string `json:"menu_item_id" format:"uuid"`
	}
}

type favoriteIDInput struct {
	MenuItemID string `path:"menu_item_id" format:"uuid"`
}

type listFavoritesInput struct {
	Day    string `query:"day" doc:"YYYY-MM-DD target day; required (controller will add defaulting)"`
	Cursor string `query:"cursor" doc:"RFC3339Nano created_at returned as next_cursor from a previous page"`
	Limit  int    `query:"limit" doc:"Page size, clamped to 1..50"`
}

type listFavoritesOutput struct {
	Body struct {
		Chips      []favoriteChipDTO `json:"chips"`
		NextCursor *string           `json:"next_cursor,omitempty"`
	}
}

// ----- Registration -----

func (a *FavoritesAPI) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "addEmployeeFavorite",
		Method:        http.MethodPost,
		Path:          "/api/employee/favorites",
		Summary:       "Mark a menu item as a favorite (idempotent)",
		Tags:          []string{"employee", "favorites"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.addFavorite)

	huma.Register(api, huma.Operation{
		OperationID:   "removeEmployeeFavorite",
		Method:        http.MethodDelete,
		Path:          "/api/employee/favorites/{menu_item_id}",
		Summary:       "Remove a favorite (idempotent — 204 even if missing)",
		Tags:          []string{"employee", "favorites"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.removeFavorite)

	huma.Register(api, huma.Operation{
		OperationID: "listEmployeeFavorites",
		Method:      http.MethodGet,
		Path:        "/api/employee/favorites",
		Summary:     "List the employee's favorites with target-day availability",
		Tags:        []string{"employee", "favorites"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.listFavorites)
}

// ----- Handlers -----

func (a *FavoritesAPI) addFavorite(ctx context.Context, in *addFavoriteInput) (*struct{}, error) {
	user, err := requireEmployeeUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Add(ctx, user.ID, in.Body.MenuItemID); err != nil {
		return nil, mapFavoriteErr(err)
	}
	return &struct{}{}, nil
}

func (a *FavoritesAPI) removeFavorite(ctx context.Context, in *favoriteIDInput) (*struct{}, error) {
	user, err := requireEmployeeUser(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.Svc.Remove(ctx, user.ID, in.MenuItemID); err != nil {
		return nil, mapFavoriteErr(err)
	}
	return &struct{}{}, nil
}

func (a *FavoritesAPI) listFavorites(ctx context.Context, in *listFavoritesInput) (*listFavoritesOutput, error) {
	user, err := requireEmployeeUser(ctx)
	if err != nil {
		return nil, err
	}
	if user.Plant == nil || *user.Plant == "" {
		return nil, huma.Error400BadRequest("plant is required (user has no plant assignment)")
	}
	if in.Day == "" {
		return nil, huma.Error400BadRequest("day is required (YYYY-MM-DD)")
	}
	if _, perr := time.Parse("2006-01-02", in.Day); perr != nil {
		return nil, huma.Error400BadRequest("day must be YYYY-MM-DD")
	}

	var cursor *time.Time
	if in.Cursor != "" {
		t, perr := time.Parse(time.RFC3339Nano, in.Cursor)
		if perr != nil {
			return nil, huma.Error400BadRequest("cursor must be RFC3339Nano timestamp")
		}
		cursor = &t
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}

	chips, next, err := a.Svc.List(ctx, user.ID, in.Day, *user.Plant, limit, cursor)
	if err != nil {
		return nil, mapFavoriteErr(err)
	}

	var resp listFavoritesOutput
	resp.Body.Chips = make([]favoriteChipDTO, 0, len(chips))
	for _, c := range chips {
		resp.Body.Chips = append(resp.Body.Chips, favoriteChipDTO{
			MenuItemID:     c.MenuItemID,
			Name:           c.Name,
			UnitPrice:      c.UnitPrice,
			VendorID:       c.VendorID,
			AvailableToday: c.AvailableToday,
		})
	}
	if next != nil {
		s := next.Format(time.RFC3339Nano)
		resp.Body.NextCursor = &s
	}
	return &resp, nil
}

// ----- helpers -----

// requireEmployeeUser is a local clone of API.requireEmployee that doesn't
// require an API receiver; defined here to keep this file self-contained per
// the P9 file allowlist (no edits to handlers.go).
func requireEmployeeUser(ctx context.Context) (*identity.User, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleEmployee {
		return nil, huma.Error403Forbidden("employee role required")
	}
	return u, nil
}

// mapFavoriteErr translates repo/service errors to huma HTTP errors. Postgres
// 23503 (foreign_key_violation) means the supplied menu_item_id doesn't exist
// (or, theoretically, the user — but the user came from a verified session).
func mapFavoriteErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return huma.Error404NotFound("menu item not found")
	}
	return huma.Error500InternalServerError("internal", err)
}

