package menu

import (
	"context"
	"time"
)

// FavoriteChip is the read-side projection returned to callers listing a
// user's favorites. Defined in the menu package (not menu/postgres) so the
// service layer can refer to it without an import cycle.
type FavoriteChip struct {
	MenuItemID     string
	Name           string
	UnitPrice      int64
	VendorID       string
	AvailableToday bool
	CreatedAt      time.Time
}

// FavoritesRepository captures the read/write contract for favorite_item rows.
// The concrete implementation lives in menu/postgres; tests may substitute a
// fake.
type FavoritesRepository interface {
	Add(ctx context.Context, userID, menuItemID string) error
	Remove(ctx context.Context, userID, menuItemID string) error
	ListByUser(ctx context.Context, userID, targetDay, plant string, limit int, cursor *time.Time) ([]FavoriteChip, *time.Time, error)
}

// FavoritesService is a thin orchestration layer over FavoritesRepository. It
// carries no business rules beyond delegating to the repo: the repo is the
// source of truth for idempotency and visibility (archived items, plant
// mapping). Kept separate from menu.Service so the existing CRUD surface and
// its tests stay untouched (P9 scope).
type FavoritesService struct {
	Repo FavoritesRepository
}

func NewFavoritesService(repo FavoritesRepository) *FavoritesService {
	return &FavoritesService{Repo: repo}
}

// Add records a favorite for the user. Idempotent on duplicate.
func (s *FavoritesService) Add(ctx context.Context, userID, menuItemID string) error {
	return s.Repo.Add(ctx, userID, menuItemID)
}

// Remove deletes the favorite. Idempotent when the row does not exist.
func (s *FavoritesService) Remove(ctx context.Context, userID, menuItemID string) error {
	return s.Repo.Remove(ctx, userID, menuItemID)
}

// List returns the user's favorites with target-day availability flags.
func (s *FavoritesService) List(
	ctx context.Context,
	userID, targetDay, plant string,
	limit int,
	cursor *time.Time,
) ([]FavoriteChip, *time.Time, error) {
	return s.Repo.ListByUser(ctx, userID, targetDay, plant, limit, cursor)
}
