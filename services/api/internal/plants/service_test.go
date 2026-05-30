package plants_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/plants"
)

// fakePlantRepo is an in-memory implementation for testing.
type fakePlantRepo struct {
	mu   sync.Mutex
	data map[string]*plants.Plant
}

func newFakeRepo() *fakePlantRepo {
	return &fakePlantRepo{data: map[string]*plants.Plant{}}
}

func (r *fakePlantRepo) List(_ context.Context, activeOnly bool) ([]*plants.Plant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*plants.Plant
	for _, p := range r.data {
		if activeOnly && !p.Active {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (r *fakePlantRepo) Get(_ context.Context, code string) (*plants.Plant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.data[code]
	if !ok {
		return nil, plants.ErrPlantNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *fakePlantRepo) Create(_ context.Context, p *plants.Plant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[p.Code]; exists {
		return plants.ErrDuplicateCode
	}
	cp := *p
	r.data[p.Code] = &cp
	return nil
}

func (r *fakePlantRepo) Update(_ context.Context, p *plants.Plant) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.data[p.Code]; !exists {
		return plants.ErrPlantNotFound
	}
	cp := *p
	r.data[p.Code] = &cp
	return nil
}

func newSvc() *plants.Service {
	return &plants.Service{Repo: newFakeRepo()}
}

func TestService_Create(t *testing.T) {
	cases := []struct {
		name      string
		code      string
		label     string
		address   string
		sortOrder int
		wantErr   bool
	}{
		{name: "happy path", code: "tn-a", label: "台南廠 A 區", address: "台南市科技路1號", sortOrder: 1},
		{name: "empty code fails", code: "", label: "label", wantErr: true},
		{name: "empty label fails", code: "tn-b", label: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSvc()
			p, err := svc.Create(context.Background(), tc.code, tc.label, tc.address, tc.sortOrder)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.code, p.Code)
			assert.Equal(t, tc.label, p.Label)
			assert.Equal(t, tc.address, p.Address)
			assert.True(t, p.Active)
		})
	}
}

func TestService_CreateDuplicate(t *testing.T) {
	svc := newSvc()
	_, err := svc.Create(context.Background(), "tn-a", "A", "", 0)
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), "tn-a", "A2", "", 0)
	assert.ErrorIs(t, err, plants.ErrDuplicateCode)
}

func TestService_Update(t *testing.T) {
	svc := newSvc()
	_, err := svc.Create(context.Background(), "tn-a", "A", "", 0)
	require.NoError(t, err)

	updated, err := svc.Update(context.Background(), "tn-a", "台南廠 A 區", "台南市", false, 5)
	require.NoError(t, err)
	assert.Equal(t, "台南廠 A 區", updated.Label)
	assert.Equal(t, "台南市", updated.Address)
	assert.False(t, updated.Active)
	assert.Equal(t, 5, updated.SortOrder)
}

func TestService_Update_NotFound(t *testing.T) {
	svc := newSvc()
	_, err := svc.Update(context.Background(), "nonexistent", "label", "", true, 0)
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)
}

// errUpdateRepo lets Get succeed but Update fail, to cover the Repo.Update error path.
type errUpdateRepo struct {
	*fakePlantRepo
	updateErr error
}

func (r *errUpdateRepo) Update(_ context.Context, _ *plants.Plant) error {
	return r.updateErr
}

func TestService_Update_RepoError(t *testing.T) {
	fake := newFakeRepo()
	fake.data["tn-a"] = &plants.Plant{Code: "tn-a", Label: "A", Active: true}
	boom := errors.New("update boom")
	svc := &plants.Service{Repo: &errUpdateRepo{fakePlantRepo: fake, updateErr: boom}}

	_, err := svc.Update(context.Background(), "tn-a", "新名", "新址", false, 3)
	assert.ErrorIs(t, err, boom)
}

func TestService_Get(t *testing.T) {
	svc := newSvc()
	_, err := svc.Create(context.Background(), "tn-a", "A", "addr", 0)
	require.NoError(t, err)

	got, err := svc.Get(context.Background(), "tn-a")
	require.NoError(t, err)
	assert.Equal(t, "tn-a", got.Code)
	assert.Equal(t, "addr", got.Address)

	_, err = svc.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)
}

func TestService_ListAll(t *testing.T) {
	svc := newSvc()
	_, _ = svc.Create(context.Background(), "tn-a", "A", "", 1)
	_, _ = svc.Create(context.Background(), "tn-b", "B", "", 2)
	_, _ = svc.Update(context.Background(), "tn-b", "B", "", false, 2) // inactive

	list, err := svc.ListAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 2) // includes inactive
}

func TestService_ListActive(t *testing.T) {
	svc := newSvc()
	_, _ = svc.Create(context.Background(), "tn-a", "A", "", 1)
	_, _ = svc.Create(context.Background(), "tn-b", "B", "", 2)
	// deactivate tn-b
	_, _ = svc.Update(context.Background(), "tn-b", "B", "", false, 2)

	list, err := svc.ListActive(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "tn-a", list[0].Code)
}

func TestService_ValidateActiveCodes(t *testing.T) {
	svc := newSvc()
	_, _ = svc.Create(context.Background(), "tn-a", "A", "", 0)
	_, _ = svc.Create(context.Background(), "tn-b", "B", "", 0)
	_, _ = svc.Update(context.Background(), "tn-b", "B", "", false, 0) // inactive

	// valid active code
	err := svc.ValidateActiveCodes(context.Background(), []string{"tn-a"})
	assert.NoError(t, err)

	// inactive code
	err = svc.ValidateActiveCodes(context.Background(), []string{"tn-b"})
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)

	// unknown code
	err = svc.ValidateActiveCodes(context.Background(), []string{"unknown"})
	assert.ErrorIs(t, err, plants.ErrPlantNotFound)

	// empty list is fine
	err = svc.ValidateActiveCodes(context.Background(), nil)
	assert.NoError(t, err)
}
