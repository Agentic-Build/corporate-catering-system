package mhttp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
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
	if _, err := a.Storage.PutObject(ctx, key, file, file.ContentType); err != nil {
		return nil, huma.Error500InternalServerError("failed to store image", err)
	}
	var resp uploadImageOutput
	resp.Body.URL = a.imageURL(key)
	return &resp, nil
}

// imageURL builds the browser-loadable URL for a stored object key. PutObject
// returns an s3://… URI that no <img> tag can render; instead we hand back a
// GET {PublicBaseURL}/uploads/{key} URL served by ServeUpload. When
// PublicBaseURL is unset the path is returned relative.
func (a *API) imageURL(key string) string {
	return strings.TrimRight(a.PublicBaseURL, "/") + "/uploads/" + key
}

// imageContentTypeByExt is the reverse of imageExtByContentType, used to set
// the response content-type when streaming a stored image back.
var imageContentTypeByExt = map[string]string{
	"jpg":  "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
}

// ServeUpload streams a previously uploaded menu image. It is a plain chi
// handler (not a huma operation) registered at GET /uploads/* — images load
// via an <img> tag with no auth header, so the route is public; only keys
// under the menu-images/ prefix are served.
func (a *API) ServeUpload(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "*")
	if !strings.HasPrefix(key, "menu-images/") || strings.Contains(key, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ext := key[strings.LastIndex(key, ".")+1:]
	ct, ok := imageContentTypeByExt[ext]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, err := a.Storage.GetObject(r.Context(), key)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer body.Close()
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, body)
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
