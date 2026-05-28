package mhttp

// Presigned upload/download for menu-item images: API authorises, client
// PUTs bytes directly to object storage with a time-bounded URL.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// maxImageBytes caps a single uploaded image at 2 MB; enforced via the
// presigned URL's ContentLength so the storage backend rejects oversize writes.
const maxImageBytes int64 = 2 << 20

// imageExtByContentType maps accepted content-types to the object-key extension.
var imageExtByContentType = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

// validateImageUpload checks content-type + size, returning the key extension.
func validateImageUpload(contentType string, size int64) (string, error) {
	ext, ok := imageExtByContentType[contentType]
	if !ok {
		return "", huma.Error400BadRequest("unsupported content type: " + contentType +
			" (allowed: image/jpeg, image/png, image/webp)")
	}
	if size <= 0 {
		return "", huma.Error400BadRequest("empty file")
	}
	if size > maxImageBytes {
		return "", huma.Error400BadRequest("file too large (max 2 MB)")
	}
	return ext, nil
}

// imageObjectKey builds a vendor-scoped object key for a menu image.
func imageObjectKey(vendorID, ext string) string {
	return fmt.Sprintf("menu-images/%s/%s.%s", vendorID, uuid.NewString(), ext)
}

// validateMenuImageKey confines a download key to menu-images/. The bucket is
// shared with payroll exports and vendor docs, so an arbitrary key would be an
// IDOR.
func validateMenuImageKey(key string) error {
	if strings.Contains(key, "..") {
		return huma.Error400BadRequest("invalid key")
	}
	if !strings.HasPrefix(key, "menu-images/") {
		return huma.Error403Forbidden("key must be under menu-images/")
	}
	return nil
}

// presignedUploadInput carries the client's declared content-type + size.
// validateImageUpload bounds both so the signed ContentLength can't be inflated.
type presignedUploadInput struct {
	Body struct {
		ContentType string `json:"content_type" doc:"image/jpeg, image/png, or image/webp" required:"true"`
		Size        int64  `json:"size" doc:"size of the upload in bytes; must be > 0 and <= 2MB" required:"true"`
	}
}

type presignedUploadOutput struct {
	Body struct {
		// URL is the presigned PUT target (PUT with matching Content-Type).
		URL string `json:"url"`
		// Key is the object key the URL writes to; echoed back when persisting.
		Key string `json:"key"`
		// ExpiresIn is the URL lifetime in seconds.
		ExpiresIn int `json:"expires_in"`
	}
}

const presignTTL = 10 * time.Minute

var (
	signCounterOnce  bool
	signCounter      metric.Int64Counter
	signErrorCounter metric.Int64Counter
)

func initSignCounters() {
	if signCounterOnce {
		return
	}
	meter := otel.GetMeterProvider().Meter("tbite.objectstore")
	signCounter, _ = meter.Int64Counter("tbite_object_signing_total",
		metric.WithDescription("Count of presigned URL requests by surface and outcome."))
	signErrorCounter, _ = meter.Int64Counter("tbite_object_signing_errors_total",
		metric.WithDescription("Count of presigned URL signing failures by surface."))
	signCounterOnce = true
}

// presignedMenuImageUpload issues a presigned PUT URL for a menu-item image.
// Caller must be a vendor operator; the key is vendor-scoped so a leaked URL
// can't write cross-vendor at the storage layer.
func (a *API) presignedMenuImageUpload(ctx context.Context, in *presignedUploadInput) (*presignedUploadOutput, error) {
	initSignCounters()
	surfaceAttr := metric.WithAttributes(attribute.String("surface", "menu-image"))

	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	ext, verr := validateImageUpload(in.Body.ContentType, in.Body.Size)
	if verr != nil {
		return nil, verr
	}
	if a.Storage == nil {
		return nil, huma.Error503ServiceUnavailable("object storage not configured")
	}
	key := imageObjectKey(vendorID, ext)
	url, err := a.Storage.PresignedPut(ctx, key, in.Body.ContentType, in.Body.Size, presignTTL)
	if err != nil {
		if signErrorCounter != nil {
			signErrorCounter.Add(ctx, 1, surfaceAttr)
		}
		return nil, huma.Error500InternalServerError("presign failed", err)
	}
	if signCounter != nil {
		signCounter.Add(ctx, 1, surfaceAttr,
			metric.WithAttributes(attribute.String("outcome", "ok")))
	}
	var resp presignedUploadOutput
	resp.Body.URL = url
	resp.Body.Key = key
	resp.Body.ExpiresIn = int(presignTTL / time.Second)
	return &resp, nil
}

// presignedMenuImageDownload returns a presigned GET URL for a stored menu
// image — signed URLs keep the API host off the bulk read path.
func (a *API) presignedMenuImageDownload(ctx context.Context, in *struct {
	Key string `query:"key" required:"true"`
}) (*presignedUploadOutput, error) {
	initSignCounters()
	surfaceAttr := metric.WithAttributes(attribute.String("surface", "menu-image-get"))

	if a.Storage == nil {
		return nil, huma.Error503ServiceUnavailable("object storage not configured")
	}
	// Require auth, but not vendor — employees also view menu images.
	if _, err := requireAuthed(ctx); err != nil {
		return nil, err
	}
	if err := validateMenuImageKey(in.Key); err != nil {
		return nil, err
	}
	url, err := a.Storage.PresignedGet(ctx, in.Key, presignTTL)
	if err != nil {
		if signErrorCounter != nil {
			signErrorCounter.Add(ctx, 1, surfaceAttr)
		}
		return nil, huma.Error500InternalServerError("presign failed", err)
	}
	if signCounter != nil {
		signCounter.Add(ctx, 1, surfaceAttr,
			metric.WithAttributes(attribute.String("outcome", "ok")))
	}
	var resp presignedUploadOutput
	resp.Body.URL = url
	resp.Body.Key = in.Key
	resp.Body.ExpiresIn = int(presignTTL / time.Second)
	return &resp, nil
}

// RegisterPresigned mounts the presigned upload/download operations.
func (a *API) RegisterPresigned(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "presignedMenuImageUpload",
		Method:        http.MethodPost,
		Path:          "/api/merchant/uploads/presigned",
		Summary:       "Issue a presigned PUT URL for a menu-item image upload",
		Tags:          []string{"merchant", "menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusOK,
	}, a.presignedMenuImageUpload)

	huma.Register(api, huma.Operation{
		OperationID:   "presignedMenuImageDownload",
		Method:        http.MethodGet,
		Path:          "/api/menu/uploads/presigned",
		Summary:       "Issue a presigned GET URL for a stored menu-item image",
		Tags:          []string{"menu"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusOK,
	}, a.presignedMenuImageDownload)
}

// requireAuthed fails when the request has no authenticated user.
// Shared by the presigned download path (both roles read menu images).
func requireAuthed(ctx context.Context) (struct{}, error) {
	if _, ok := idhttp.UserFromContext(ctx); !ok {
		return struct{}{}, huma.Error401Unauthorized("not authenticated")
	}
	return struct{}{}, nil
}
