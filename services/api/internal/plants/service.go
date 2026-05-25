package plants

import (
	"context"
	"fmt"
	"strings"
)

// Service manages plant registry operations.
type Service struct {
	Repo Repository
}

// ListActive returns active plants ordered by sort_order, code.
func (s *Service) ListActive(ctx context.Context) ([]*Plant, error) {
	return s.Repo.List(ctx, true)
}

// ListAll returns all plants (including inactive) for admin use.
func (s *Service) ListAll(ctx context.Context) ([]*Plant, error) {
	return s.Repo.List(ctx, false)
}

// Get returns a plant by code.
func (s *Service) Get(ctx context.Context, code string) (*Plant, error) {
	return s.Repo.Get(ctx, code)
}

// Create creates a new plant. Code must be non-empty.
func (s *Service) Create(ctx context.Context, code, label, address string, sortOrder int) (*Plant, error) {
	code = strings.TrimSpace(code)
	label = strings.TrimSpace(label)
	if code == "" {
		return nil, fmt.Errorf("%w: code is required", ErrDuplicateCode)
	}
	if label == "" {
		return nil, fmt.Errorf("%w: label is required", ErrPlantNotFound)
	}
	p := &Plant{
		Code:      code,
		Label:     label,
		Address:   strings.TrimSpace(address),
		Active:    true,
		SortOrder: sortOrder,
	}
	if err := s.Repo.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// Update updates an existing plant's label, address, active flag, and sort_order.
func (s *Service) Update(ctx context.Context, code, label, address string, active bool, sortOrder int) (*Plant, error) {
	p, err := s.Repo.Get(ctx, code)
	if err != nil {
		return nil, err
	}
	p.Label = strings.TrimSpace(label)
	p.Address = strings.TrimSpace(address)
	p.Active = active
	p.SortOrder = sortOrder
	if err := s.Repo.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ValidateActiveCodes checks each code exists and is active. Returns first invalid code.
func (s *Service) ValidateActiveCodes(ctx context.Context, codes []string) error {
	for _, code := range codes {
		p, err := s.Repo.Get(ctx, code)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrPlantNotFound, code)
		}
		if !p.Active {
			return fmt.Errorf("%w: %s is inactive", ErrPlantNotFound, code)
		}
	}
	return nil
}
