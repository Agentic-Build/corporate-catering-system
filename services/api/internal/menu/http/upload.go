package mhttp

import (
	"context"
	"fmt"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

// maxImageBytes caps a single uploaded image at 2 MB.
const maxImageBytes int64 = 2 << 20

// imageExtByContentType maps the accepted image content-types to the file
// extension used in the generated object key.
var imageExtByContentType = map[string]string{
	"image/jpeg": "jpg",
	"image/png":  "png",
	"image/webp": "webp",
}

// uploadImageInput is the multipart/form-data body for uploadMerchantImage.
// The single "file" form field carries the image; huma's MimeTypeValidator
// enforces the contentType allow-list declared in the tag.
type uploadImageInput struct {
	RawBody huma.MultipartFormFiles[struct {
		File huma.FormFile `form:"file" contentType:"image/jpeg,image/png,image/webp" required:"true"`
	}]
}

type uploadImageOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

// uploadImage validates an uploaded image (content-type + size), stores it
// under a vendor-scoped object key, and returns the stored URL.
func (a *API) uploadImage(ctx context.Context, in *uploadImageInput) (*uploadImageOutput, error) {
	_, vendorID, err := a.requireVendor(ctx)
	if err != nil {
		return nil, err
	}
	file := in.RawBody.Data().File
	ext, verr := validateImageUpload(file.ContentType, file.Size)
	if verr != nil {
		return nil, verr
	}
	key := imageObjectKey(vendorID, ext)
	url, err := a.Storage.PutObject(ctx, key, file, file.ContentType)
	if err != nil {
		return nil, huma.Error500InternalServerError("failed to store image", err)
	}
	var resp uploadImageOutput
	resp.Body.URL = url
	return &resp, nil
}

// validateImageUpload checks the content-type is an accepted image format and
// the size is within maxImageBytes, returning the extension for the object key.
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
