package mhttp

// Presigned upload path for menu-item images (architecture issue #60).
// The API authorises the operation and returns a time-bounded URL the
// client uses to PUT bytes directly to object storage. The legacy
// multipart upload at POST /api/merchant/uploads remains wired for
// backwards compatibility but is deprecated; new clients should use
// POST /api/merchant/uploads/presigned and PUT directly.

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
)

// maxImageBytes caps a single uploaded image at 2 MB. Enforced via the
// presigned URL's ContentLength so the storage backend rejects writes
// larger than the signed bound.
const maxImageBytes int64 = 2 << 20

// imageExtByContentType maps the accepted image content-types to the
// extension used in the generated object key.
var imageExtByContentType = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

// validateImageUpload checks the content-type is an accepted image
// format and the size is within maxImageBytes, returning the
// extension for the object key.
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

// presignedUploadInput carries the client's declared content-type and
// byte size. The handler validates both against the same policy used
// by the legacy memory-path upload (validateImageUpload) so policy
// stays uniform across transports.
type presignedUploadInput struct {
	Body struct {
		ContentType string `json:"content_type" doc:"image/jpeg, image/png, or image/webp" required:"true"`
		Size        int64  `json:"size" doc:"size of the upload in bytes; must be > 0 and <= 2MB" required:"true"`
	}
}

type presignedUploadOutput struct {
	Body struct {
		// URL is the presigned PUT target. Client must use HTTP PUT
		// with Content-Type matching the request body.
		URL string `json:"url"`
		// Key is the object key the URL writes to. Returned so the
		// client can echo it back to the application when persisting
		// the image reference on the menu item.
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

// presignedMenuImageUpload issues a presigned PUT URL for a menu-item
// image. The caller must be a vendor operator. The object key is
// scoped to the caller's vendor so cross-vendor writes are
// impossible at the storage layer even if the URL is leaked.
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

// presignedMenuImageDownload returns a presigned GET URL for an
// already-stored menu image. Public read of menu-images/* is allowed
// through ServeUpload too, but BYO object-storage modes (issue #60)
// prefer signed URLs so the API host is not on the bulk path.
func (a *API) presignedMenuImageDownload(ctx context.Context, in *struct {
	Key string `query:"key" required:"true"`
}) (*presignedUploadOutput, error) {
	initSignCounters()
	surfaceAttr := metric.WithAttributes(attribute.String("surface", "menu-image-get"))

	if a.Storage == nil {
		return nil, huma.Error503ServiceUnavailable("object storage not configured")
	}
	// Require auth; we deliberately do not require vendor since
	// employees view menu images.
	if _, err := requireAuthed(ctx); err != nil {
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
// Wired separately from Register() so the chart's BYO-storage mode can
// skip the legacy multipart endpoint without dropping these.
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

// requireAuthed is a thin helper that fails when the request has no
// authenticated user. The vendor/employee specialisations live in
// handlers.go; this one is shared by the presigned download path
// because both roles legitimately read menu images.
func requireAuthed(ctx context.Context) (struct{}, error) {
	if _, ok := idhttp.UserFromContext(ctx); !ok {
		return struct{}{}, huma.Error401Unauthorized("not authenticated")
	}
	return struct{}{}, nil
}
