package vendor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

// Clock abstracts time.Now for deterministic invite expiry.
type Clock interface{ Now() time.Time }

// Service orchestrates admin operations over vendors, plant mappings, and invites.
// It depends only on repository interfaces and a clock; no transport concerns.
type Service struct {
	Vendors   Repository
	Plants    PlantMappingRepository
	Invites   identity.VendorInviteRepository
	Clock     Clock
	InviteTTL time.Duration
}

// CreatePending creates a vendor in pending status. Approval (and plant mapping)
// is a separate admin step.
func (s *Service) CreatePending(ctx context.Context, displayName, legalName, email string) (*Vendor, error) {
	v := &Vendor{
		DisplayName:  displayName,
		LegalName:    legalName,
		ContactEmail: email,
		Status:       StatusPending,
	}
	if err := s.Vendors.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// Approve moves a pending or suspended vendor to approved and replaces its
// plant mapping with the provided set.
func (s *Service) Approve(ctx context.Context, id, adminUserID string, plants []string) error {
	v, err := s.Vendors.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if v.Status == StatusApproved {
		return ErrAlreadyApproved
	}
	if v.Status != StatusPending && v.Status != StatusSuspended {
		return ErrInvalidStatus
	}
	if err := s.Vendors.UpdateStatus(ctx, id, StatusApproved, &adminUserID); err != nil {
		return err
	}
	return s.Plants.Set(ctx, id, plants)
}

// Suspend transitions an approved vendor to suspended.
func (s *Service) Suspend(ctx context.Context, id string) error {
	v, err := s.Vendors.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if v.Status != StatusApproved {
		return ErrInvalidStatus
	}
	return s.Vendors.UpdateStatus(ctx, id, StatusSuspended, nil)
}

// Reinstate transitions a suspended vendor back to approved.
func (s *Service) Reinstate(ctx context.Context, id, adminUserID string) error {
	v, err := s.Vendors.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if v.Status != StatusSuspended {
		return ErrInvalidStatus
	}
	return s.Vendors.UpdateStatus(ctx, id, StatusApproved, &adminUserID)
}

// List returns vendors filtered by status (empty slice means all).
func (s *Service) List(ctx context.Context, statuses []Status) ([]*Vendor, error) {
	return s.Vendors.List(ctx, statuses)
}

// ListPlants returns the active plant codes mapped to a vendor.
func (s *Service) ListPlants(ctx context.Context, id string) ([]string, error) {
	list, err := s.Plants.ListByVendor(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	for _, p := range list {
		if p.Active {
			out = append(out, p.Plant)
		}
	}
	return out, nil
}

// IssueInvite generates a single-use invite code stored in vendor_invite.
// The vendor must exist; expiry is now + InviteTTL.
func (s *Service) IssueInvite(ctx context.Context, vendorID string) (string, error) {
	if _, err := s.Vendors.GetByID(ctx, vendorID); err != nil {
		return "", err
	}
	code := makeInviteCode()
	inv := &identity.VendorInvite{
		Code:      code,
		VendorID:  vendorID,
		ExpiresAt: s.Clock.Now().Add(s.InviteTTL),
	}
	if err := s.Invites.Put(ctx, inv); err != nil {
		return "", err
	}
	return code, nil
}

func makeInviteCode() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "TBI-" + base64.RawURLEncoding.EncodeToString(b)
}
