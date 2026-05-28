package mhttp

// Direct multipart upload for menu-item images. The client POSTs a
// multipart/form-data body with a "file" field; the handler validates,
// streams to object storage, and returns the permanent public URL.
//
// This satisfies the ImageUploader.svelte → /api/uploads proxy →
// POST /api/merchant/uploads chain. The presigned-URL path (presign.go)
// remains for clients that prefer client-side PUT directly to MinIO.

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

const contentTypeHeader = "Content-Type"

// PublicURL assembles the permanent browser-reachable URL for an object key.
// MinIO path-style: {publicBaseURL}/{bucket}/{key}.
func (a *API) PublicURL(key string) string {
	base := strings.TrimRight(a.StoragePublicBaseURL, "/")
	return fmt.Sprintf("%s/%s/%s", base, a.StorageBucket, key)
}

// HandleDirectUpload is the handler for POST /api/merchant/uploads.
// It is registered as a plain chi route in main.go via extraRoutes so we
// avoid Huma's multipart limitations while still benefiting from the chi
// identity middleware that already populates the request context.
func (a *API) HandleDirectUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxImageBytes + 512); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "cannot parse multipart form")
		return
	}

	_, vendorID, err := a.requireVendor(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		if he, ok := err.(huma.StatusError); ok {
			status = he.GetStatus()
		}
		writeJSONErr(w, status, err.Error())
		return
	}

	if a.Storage == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "object storage not configured")
		return
	}

	f, fh, err := r.FormFile("file")
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer f.Close()

	ext, verr := validateImageUpload(fh.Header.Get(contentTypeHeader), fh.Size)
	if verr != nil {
		status := http.StatusBadRequest
		if he, ok := verr.(huma.StatusError); ok {
			status = he.GetStatus()
		}
		writeJSONErr(w, status, verr.Error())
		return
	}

	key := imageObjectKey(vendorID, ext)
	contentType := fh.Header.Get(contentTypeHeader)

	limited := io.LimitReader(f, maxImageBytes+1)
	if _, err := a.Storage.PutObject(r.Context(), key, limited, contentType); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "upload failed")
		return
	}

	publicURL := a.PublicURL(key)
	w.Header().Set(contentTypeHeader, "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"url":%q}`, publicURL)
}

func writeJSONErr(w http.ResponseWriter, status int, detail string) {
	w.Header().Set(contentTypeHeader, "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"status":%d,"detail":%q}`, status, detail)
}
