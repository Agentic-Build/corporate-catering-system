// Merchant-facing compliance endpoints: a vendor_operator views its own vendor
// status, documents, and computed compliance warnings, and may upload or
// resupply its own compliance documents (self-service 補件).
package chttp

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/compliance"
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
	_, vendorID, err := idhttp.RequireVendor(ctx)
	return vendorID, err
}

// ----- Registration -----

// registerMerchant attaches the merchant compliance self-view + self-service
// document upload routes. Called from API.Register so main.go needs no extra
// wiring.
func (a *API) registerMerchant(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getMerchantCompliance",
		Method:      http.MethodGet,
		Path:        "/api/merchant/compliance",
		Summary:     "View own vendor compliance status, documents, and warnings",
		Tags:        []string{"merchant", "compliance"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.merchantCompliance)

	huma.Register(api, huma.Operation{
		OperationID:   "uploadMerchantDocument",
		Method:        http.MethodPost,
		Path:          "/api/merchant/documents",
		Summary:       "Upload or resupply a compliance document for own vendor",
		Tags:          []string{"merchant", "compliance"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusCreated,
	}, a.merchantUploadDocument)
}

// merchantUploadDocumentInput is the base64-in-JSON upload body for a vendor's
// own document. Supersedes, when set, makes this a resupply of an existing
// document (must be the same vendor's, and already reviewed).
type merchantUploadDocumentInput struct {
	Body struct {
		Kind          string  `json:"kind" enum:"business_license,food_safety_permit,tax_registration,insurance,other"`
		Filename      string  `json:"filename" minLength:"1"`
		ContentBase64 string  `json:"content_base64" minLength:"1"`
		ExpiresAt     *string `json:"expires_at,omitempty"`
		Supersedes    *string `json:"supersedes,omitempty" format:"uuid" doc:"ID of the document this upload replaces (resupply)"`
	}
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

// merchantUploadDocument lets a vendor_operator upload (or resupply) a
// compliance document for its own vendor. The vendor_id is resolved from the
// session, never from input, so a merchant can only ever touch its own docs.
func (a *API) merchantUploadDocument(ctx context.Context, in *merchantUploadDocumentInput) (*uploadDocumentOutput, error) {
	vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	u, _ := idhttp.UserFromContext(ctx)
	body, err := base64.StdEncoding.DecodeString(in.Body.ContentBase64)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid base64 content")
	}
	var expires *time.Time
	if in.Body.ExpiresAt != nil && *in.Body.ExpiresAt != "" {
		t, perr := time.ParseInLocation("2006-01-02", *in.Body.ExpiresAt, time.UTC)
		if perr != nil {
			return nil, huma.Error400BadRequest("expires_at must be YYYY-MM-DD")
		}
		expires = &t
	}
	d, err := a.Svc.UploadDocument(ctx, compliance.UploadInput{
		VendorID:   vendorID,
		Kind:       compliance.DocumentKind(in.Body.Kind),
		Filename:   in.Body.Filename,
		Body:       strings.NewReader(string(body)),
		ExpiresAt:  expires,
		UploadedBy: u.ID,
		ActorRole:  "vendor_operator",
		Supersedes: in.Body.Supersedes,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp uploadDocumentOutput
	resp.Body.Document = docToDTO(d)
	return &resp, nil
}
