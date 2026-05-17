// Merchant-facing compliance endpoints. Read-only: a vendor_operator views its
// own vendor status, documents, and computed compliance warnings. Document
// upload remains admin-only (see design §6.3).
package chttp

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// ----- DTOs -----

type vendorInfoDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Status      string `json:"status"`
}

type warningDTO struct {
	Kind     string `json:"kind"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type merchantComplianceOutput struct {
	Body struct {
		Vendor    vendorInfoDTO `json:"vendor"`
		Documents []documentDTO `json:"documents"`
		Warnings  []warningDTO  `json:"warnings"`
	}
}

func warningToDTO(w compliance.Warning) warningDTO {
	return warningDTO{
		Kind:     w.Kind,
		Message:  w.Message,
		Severity: string(w.Severity),
	}
}

// ----- Auth -----

// requireVendor enforces a vendor_operator bound to a vendor and returns the
// resolved vendor_id from the session (never a path param).
func (a *API) requireVendor(ctx context.Context) (string, error) {
	u, ok := idhttp.UserFromContext(ctx)
	if !ok {
		return "", huma.Error401Unauthorized("not authenticated")
	}
	if u.Role != identity.RoleVendorOperator {
		return "", huma.Error403Forbidden("vendor operator required")
	}
	if u.VendorID == nil || *u.VendorID == "" {
		return "", huma.Error403Forbidden("user is not bound to a vendor")
	}
	return *u.VendorID, nil
}

// ----- Registration -----

// registerMerchant attaches the merchant compliance self-view route. It is
// called from API.Register so main.go needs no extra wiring.
func (a *API) registerMerchant(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getMerchantCompliance",
		Method:      http.MethodGet,
		Path:        "/api/merchant/compliance",
		Summary:     "View own vendor compliance status, documents, and warnings",
		Tags:        []string{"merchant", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.merchantCompliance)
}

// ----- Handlers -----

func (a *API) merchantCompliance(ctx context.Context, _ *struct{}) (*merchantComplianceOutput, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := a.Svc.MerchantCompliance(ctx, vendorID)
	if err != nil {
		return nil, mapErr(err)
	}
	var resp merchantComplianceOutput
	resp.Body.Vendor = vendorInfoDTO{
		ID:          summary.Vendor.ID,
		DisplayName: summary.Vendor.DisplayName,
		Status:      summary.Vendor.Status,
	}
	resp.Body.Documents = make([]documentDTO, 0, len(summary.Documents))
	for _, d := range summary.Documents {
		resp.Body.Documents = append(resp.Body.Documents, docToDTO(d))
	}
	resp.Body.Warnings = make([]warningDTO, 0, len(summary.Warnings))
	for _, w := range summary.Warnings {
		resp.Body.Warnings = append(resp.Body.Warnings, warningToDTO(w))
	}
	return &resp, nil
}
