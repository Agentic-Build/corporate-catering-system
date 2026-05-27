package mhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	"github.com/takalawang/corporate-catering-system/services/api/internal/platform/storage"
)

// --- helpers ----------------------------------------------------------------

func vendorCtx(vendorID string) context.Context {
	uid := "test-user"
	u := &identity.User{
		ID:           uid,
		Role:         identity.RoleVendorOperator,
		PrimaryEmail: "vendor@test.test",
		VendorID:     &vendorID,
	}
	return idhttp.ContextWithUser(context.Background(), u)
}

func noAuthCtx() context.Context { return context.Background() }

func buildMultipart(t *testing.T, fieldName, filename, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(fieldName, filename)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	w.Close()
	return &buf, w.FormDataContentType()
}

// --- PublicURL ---------------------------------------------------------------

func TestPublicURL(t *testing.T) {
	a := &API{StoragePublicBaseURL: "http://minio.tbite.local", StorageBucket: "tbite-dev"}
	got := a.PublicURL("menu-images/v1/abc.jpg")
	want := "http://minio.tbite.local/tbite-dev/menu-images/v1/abc.jpg"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPublicURL_TrailingSlash(t *testing.T) {
	a := &API{StoragePublicBaseURL: "https://files.example.com/", StorageBucket: "prod"}
	got := a.PublicURL("brand/items/i001.jpg")
	want := "https://files.example.com/prod/brand/items/i001.jpg"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// --- validateImageUpload (reused in upload path) ----------------------------

func TestValidateImageUpload_OK(t *testing.T) {
	for _, ct := range []string{"image/jpeg", "image/png", "image/webp"} {
		if _, err := validateImageUpload(ct, 1024); err != nil {
			t.Fatalf("ct=%s: unexpected error: %v", ct, err)
		}
	}
}

func TestValidateImageUpload_BadType(t *testing.T) {
	if _, err := validateImageUpload("application/pdf", 512); err == nil {
		t.Fatal("expected error for pdf content-type")
	}
}

func TestValidateImageUpload_Empty(t *testing.T) {
	if _, err := validateImageUpload("image/jpeg", 0); err == nil {
		t.Fatal("expected error for zero size")
	}
}

func TestValidateImageUpload_TooLarge(t *testing.T) {
	if _, err := validateImageUpload("image/jpeg", maxImageBytes+1); err == nil {
		t.Fatal("expected error for oversized file")
	}
}

// --- validateMenuImageKey (presigned download guard) ------------------------

func TestValidateMenuImageKey_Allowed(t *testing.T) {
	for _, k := range []string{"menu-images/v1/abc.jpg", "menu-images/x/y.png"} {
		if err := validateMenuImageKey(k); err != nil {
			t.Fatalf("key %q should be allowed: %v", k, err)
		}
	}
}

func TestValidateMenuImageKey_Rejected(t *testing.T) {
	// Keys outside menu-images/ (shared bucket holds payroll/vendor docs) and traversal must be rejected.
	for _, k := range []string{
		"payroll/batch-1.csv",
		"vendor-docs/v1/contract.pdf",
		"menu-images/../payroll/batch-1.csv",
		"menu-imagesX/abc.jpg",
		"",
	} {
		if err := validateMenuImageKey(k); err == nil {
			t.Fatalf("key %q should be rejected", k)
		}
	}
}

// --- HandleDirectUpload -----------------------------------------------------

// stubStorage records what was put and returns the s3:// URI.
type stubStorage struct {
	putCalled bool
	putKey    string
}

func (s *stubStorage) PutObject(_ context.Context, key string, _ io.Reader, _ string) (string, error) {
	s.putCalled = true
	s.putKey = key
	return fmt.Sprintf("s3://tbite/%s", key), nil
}

// We need API.Storage to satisfy the storage.S3Client interface — but S3Client
// is a concrete struct, not an interface. The handler calls a.Storage.PutObject
// directly. To keep the test pure (no Docker), we patch via embedding: we
// temporarily replace the handler to call the stub. Instead, let's test through
// a real *storage.S3Client is impractical without Docker, so we test everything
// up to the PutObject call by pointing Storage at a nil and checking the 503,
// and separately verify the auth path.

func TestHandleDirectUpload_NoAuth(t *testing.T) {
	a := &API{StoragePublicBaseURL: "http://minio.tbite.local", StorageBucket: "tbite-dev"}

	body, ct := buildMultipart(t, "file", "photo.jpg", "image/jpeg", []byte("fake-jpeg"))
	req := httptest.NewRequest(http.MethodPost, "/api/merchant/uploads", body)
	req = req.WithContext(noAuthCtx())
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDirectUpload_NoStorage(t *testing.T) {
	vid := "vendor-1"
	a := &API{
		Storage:              nil,
		StoragePublicBaseURL: "http://minio.tbite.local",
		StorageBucket:        "tbite-dev",
	}

	body, ct := buildMultipart(t, "file", "photo.jpg", "image/jpeg", []byte("fake-jpeg"))
	req := httptest.NewRequest(http.MethodPost, "/api/merchant/uploads", body)
	req = req.WithContext(vendorCtx(vid))
	req.Header.Set("Content-Type", ct)

	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDirectUpload_MissingFile(t *testing.T) {
	vid := "vendor-1"
	a := &API{
		Storage:              &storage.S3Client{}, // non-nil but won't be called
		StoragePublicBaseURL: "http://minio.tbite.local",
		StorageBucket:        "tbite-dev",
	}

	// Send a body without a "file" field.
	req := httptest.NewRequest(http.MethodPost, "/api/merchant/uploads",
		strings.NewReader(""))
	req = req.WithContext(vendorCtx(vid))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xxx")

	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleDirectUpload_UnsupportedContentType(t *testing.T) {
	vid := "vendor-1"
	a := &API{
		Storage:              &storage.S3Client{},
		StoragePublicBaseURL: "http://minio.tbite.local",
		StorageBucket:        "tbite-dev",
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, _ := mw.CreateFormFile("file", "evil.pdf")
	part.Write([]byte("pdf content"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/merchant/uploads", &buf)
	req = req.WithContext(vendorCtx(vid))
	req.Header.Set("Content-Type", mw.FormDataContentType())

	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, req)

	// multipart.FileHeader.Header["Content-Type"] will be
	// "application/octet-stream" for a CreateFormFile part without
	// explicit content-type, which is not in the allow-list → 400.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 got %d body=%s", rec.Code, rec.Body.String())
	}
}
