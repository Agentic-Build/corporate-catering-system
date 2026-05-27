package menu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/takalawang/corporate-catering-system/services/api/internal/menu"
)

// fakeFavoritesRepo is an in-memory FavoritesRepository for the service layer.
// errOn, when set, makes the matching method return the configured error.
type fakeFavoritesRepo struct {
	added       map[string]map[string]struct{} // userID -> set(menuItemID)
	listResult  []menu.FavoriteChip
	listCursor  *time.Time
	lastList    favListArgs
	errOnAdd    error
	errOnRemove error
	errOnList   error
}

type favListArgs struct {
	userID, targetDay, plant string
	limit                    int
	cursor                   *time.Time
}

func newFakeFavoritesRepo() *fakeFavoritesRepo {
	return &fakeFavoritesRepo{added: map[string]map[string]struct{}{}}
}

func (r *fakeFavoritesRepo) Add(_ context.Context, userID, menuItemID string) error {
	if r.errOnAdd != nil {
		return r.errOnAdd
	}
	if r.added[userID] == nil {
		r.added[userID] = map[string]struct{}{}
	}
	r.added[userID][menuItemID] = struct{}{}
	return nil
}

func (r *fakeFavoritesRepo) Remove(_ context.Context, userID, menuItemID string) error {
	if r.errOnRemove != nil {
		return r.errOnRemove
	}
	if set := r.added[userID]; set != nil {
		delete(set, menuItemID)
	}
	return nil
}

func (r *fakeFavoritesRepo) ListByUser(_ context.Context, userID, targetDay, plant string, limit int, cursor *time.Time) ([]menu.FavoriteChip, *time.Time, error) {
	r.lastList = favListArgs{userID, targetDay, plant, limit, cursor}
	if r.errOnList != nil {
		return nil, nil, r.errOnList
	}
	return r.listResult, r.listCursor, nil
}

func TestFavoritesService_Add_Delegates(t *testing.T) {
	repo := newFakeFavoritesRepo()
	svc := menu.NewFavoritesService(repo)
	require.NoError(t, svc.Add(context.Background(), "u1", "item-1"))
	_, ok := repo.added["u1"]["item-1"]
	assert.True(t, ok)
}

func TestFavoritesService_Add_PropagatesError(t *testing.T) {
	repo := newFakeFavoritesRepo()
	repo.errOnAdd = errors.New("boom")
	svc := menu.NewFavoritesService(repo)
	assert.Error(t, svc.Add(context.Background(), "u1", "item-1"))
}

func TestFavoritesService_Remove_Delegates(t *testing.T) {
	repo := newFakeFavoritesRepo()
	svc := menu.NewFavoritesService(repo)
	require.NoError(t, svc.Add(context.Background(), "u1", "item-1"))
	require.NoError(t, svc.Remove(context.Background(), "u1", "item-1"))
	_, ok := repo.added["u1"]["item-1"]
	assert.False(t, ok)
}

func TestFavoritesService_Remove_PropagatesError(t *testing.T) {
	repo := newFakeFavoritesRepo()
	repo.errOnRemove = errors.New("boom")
	svc := menu.NewFavoritesService(repo)
	assert.Error(t, svc.Remove(context.Background(), "u1", "item-1"))
}

func TestFavoritesService_List_DelegatesAndReturns(t *testing.T) {
	repo := newFakeFavoritesRepo()
	next := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo.listResult = []menu.FavoriteChip{
		{MenuItemID: "item-1", Name: "雞腿便當", UnitPrice: 110, VendorID: "v1", AvailableToday: true},
	}
	repo.listCursor = &next
	svc := menu.NewFavoritesService(repo)

	cursor := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	chips, gotCursor, err := svc.List(context.Background(), "u1", "2026-05-15", "F12B-3F", 10, &cursor)
	require.NoError(t, err)
	require.Len(t, chips, 1)
	assert.Equal(t, "item-1", chips[0].MenuItemID)
	assert.Equal(t, int64(110), chips[0].UnitPrice)
	require.NotNil(t, gotCursor)
	assert.Equal(t, next, *gotCursor)
	// Args pass through untouched.
	assert.Equal(t, favListArgs{"u1", "2026-05-15", "F12B-3F", 10, &cursor}, repo.lastList)
}

func TestFavoritesService_List_PropagatesError(t *testing.T) {
	repo := newFakeFavoritesRepo()
	repo.errOnList = errors.New("boom")
	svc := menu.NewFavoritesService(repo)
	_, _, err := svc.List(context.Background(), "u1", "2026-05-15", "F12B-3F", 10, nil)
	assert.Error(t, err)
}
