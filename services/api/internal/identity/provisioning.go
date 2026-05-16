package identity

import "context"

type VendorOperatorProvisionInput struct {
	Email       string
	DisplayName string
	VendorID    string
	Active      bool
}

type VendorOperatorProvisioned struct {
	Provider        string
	ExternalSubject string
	SetupURL        string
}

type VendorOperatorProvisioner interface {
	UpsertVendorOperator(ctx context.Context, in VendorOperatorProvisionInput) (*VendorOperatorProvisioned, error)
	SuspendVendorOperator(ctx context.Context, provider, externalSubject string) error
	ReinstateVendorOperator(ctx context.Context, provider, externalSubject, vendorID string) error
}
