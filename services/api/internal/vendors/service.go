package vendor

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

// Service orchestrates admin operations over vendors, plant mappings, and the
// Authentik-backed vendor-operator mirror.
type Service struct {
	Vendors     Repository
	Plants      PlantMappingRepository
	Operators   OperatorRepository
	Provisioner identity.VendorOperatorProvisioner
	Users       identity.UserRepository
	Sessions    identity.SessionStore
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
	ops, err := s.Operators.ListByVendorStatus(ctx, id, []OperatorStatus{OperatorStatusActive})
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := s.suspendRemoteOperator(ctx, op); err != nil {
			return err
		}
	}
	if err := s.Vendors.UpdateStatus(ctx, id, StatusSuspended, nil); err != nil {
		return err
	}
	if err := s.Operators.SetStatuses(ctx, id, []OperatorStatus{OperatorStatusActive}, OperatorStatusVendorSuspended); err != nil {
		return err
	}
	for _, op := range ops {
		if err := s.revokeByEmail(ctx, op.Email); err != nil {
			return err
		}
	}
	return nil
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
	ops, err := s.Operators.ListByVendorStatus(ctx, id, []OperatorStatus{OperatorStatusVendorSuspended})
	if err != nil {
		return err
	}
	for _, op := range ops {
		if err := s.reinstateRemoteOperator(ctx, op); err != nil {
			return err
		}
	}
	if err := s.Vendors.UpdateStatus(ctx, id, StatusApproved, &adminUserID); err != nil {
		return err
	}
	return s.Operators.SetStatuses(ctx, id, []OperatorStatus{OperatorStatusVendorSuspended}, OperatorStatusActive)
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

func (s *Service) ListOperators(ctx context.Context, vendorID string) ([]*OperatorAccount, error) {
	if _, err := s.Vendors.GetByID(ctx, vendorID); err != nil {
		return nil, err
	}
	return s.Operators.ListByVendor(ctx, vendorID)
}

func (s *Service) CreateOperator(ctx context.Context, vendorID, email, displayName string) (*OperatorAccount, error) {
	v, err := s.Vendors.GetByID(ctx, vendorID)
	if err != nil {
		return nil, err
	}
	if v.Status != StatusApproved {
		return nil, ErrInvalidStatus
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return nil, fmt.Errorf("%w: display_name is required", ErrInvalidOperator)
	}
	if s.Provisioner == nil {
		return nil, fmt.Errorf("%w: provisioner is not configured", ErrProvisioningSetup)
	}
	prov, err := s.Provisioner.UpsertVendorOperator(ctx, identity.VendorOperatorProvisionInput{
		Email:       email,
		DisplayName: displayName,
		VendorID:    vendorID,
		Active:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProvisioningSetup, err)
	}
	now := time.Now().UTC()
	op := &OperatorAccount{
		VendorID:        vendorID,
		Email:           email,
		DisplayName:     displayName,
		Provider:        prov.Provider,
		ExternalSubject: stringPtr(prov.ExternalSubject),
		Status:          OperatorStatusActive,
		SetupURL:        stringPtr(prov.SetupURL),
		LastSyncedAt:    &now,
	}
	if err := s.Operators.Upsert(ctx, op); err != nil {
		return nil, err
	}
	return op, nil
}

func (s *Service) SuspendOperator(ctx context.Context, vendorID, operatorID string) error {
	op, err := s.Operators.Get(ctx, vendorID, operatorID)
	if err != nil {
		return err
	}
	if op.Status != OperatorStatusActive {
		return ErrInvalidStatus
	}
	if err := s.suspendRemoteOperator(ctx, op); err != nil {
		return err
	}
	if err := s.Operators.SetStatus(ctx, vendorID, operatorID, OperatorStatusSuspended); err != nil {
		return err
	}
	return s.revokeByEmail(ctx, op.Email)
}

func (s *Service) ReinstateOperator(ctx context.Context, vendorID, operatorID string) error {
	v, err := s.Vendors.GetByID(ctx, vendorID)
	if err != nil {
		return err
	}
	if v.Status != StatusApproved {
		return ErrInvalidStatus
	}
	op, err := s.Operators.Get(ctx, vendorID, operatorID)
	if err != nil {
		return err
	}
	if op.Status != OperatorStatusSuspended && op.Status != OperatorStatusVendorSuspended {
		return ErrInvalidStatus
	}
	if err := s.reinstateRemoteOperator(ctx, op); err != nil {
		return err
	}
	return s.Operators.SetStatus(ctx, vendorID, operatorID, OperatorStatusActive)
}

func (s *Service) suspendRemoteOperator(ctx context.Context, op *OperatorAccount) error {
	if s.Provisioner == nil {
		return fmt.Errorf("%w: provisioner is not configured", ErrProvisioningSetup)
	}
	if op.ExternalSubject == nil || *op.ExternalSubject == "" {
		return fmt.Errorf("%w: missing external subject", ErrInvalidOperator)
	}
	if err := s.Provisioner.SuspendVendorOperator(ctx, op.Provider, *op.ExternalSubject); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisioningSetup, err)
	}
	return nil
}

func (s *Service) reinstateRemoteOperator(ctx context.Context, op *OperatorAccount) error {
	if s.Provisioner == nil {
		return fmt.Errorf("%w: provisioner is not configured", ErrProvisioningSetup)
	}
	if op.ExternalSubject == nil || *op.ExternalSubject == "" {
		return fmt.Errorf("%w: missing external subject", ErrInvalidOperator)
	}
	if err := s.Provisioner.ReinstateVendorOperator(ctx, op.Provider, *op.ExternalSubject, op.VendorID); err != nil {
		return fmt.Errorf("%w: %v", ErrProvisioningSetup, err)
	}
	return nil
}

func (s *Service) revokeByEmail(ctx context.Context, email string) error {
	if s.Users == nil || s.Sessions == nil {
		return nil
	}
	u, err := s.Users.GetByEmail(ctx, email)
	if errors.Is(err, identity.ErrUserNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.Sessions.RevokeAllForUser(ctx, u.ID)
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return "", fmt.Errorf("%w: email is required", ErrInvalidOperator)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", fmt.Errorf("%w: invalid email", ErrInvalidOperator)
	}
	return email, nil
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
