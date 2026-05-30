package mhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu"
	mhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/menu/http"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/clock"
	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/platform/storage"
)

// newS3 builds a real S3Client whose endpoint points at the supplied URL. The
// presign methods build URLs without any network I/O; PutObject actually issues
// an HTTP request to endpoint, so callers control success vs. error by giving an
// httptest server that returns 200 or 500.
func newS3(t *testing.T, endpoint string) *storage.S3Client {
	t.Helper()
	c, err := storage.NewS3(context.Background(), storage.S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		Bucket:          "tbite-test",
		UsePathStyle:    true,
	})
	require.NoError(t, err)
	return c
}

// --- presign upload success (presign.go imageObjectKey + PresignedPut path) ---

func TestPresignUpload_OK(t *testing.T) {
	srv := buildPresign(t, vendorUser(), newS3(t, "http://127.0.0.1:9"))
	resp := do(t, http.MethodPost, srv.URL+"/api/merchant/uploads/presigned",
		`{"content_type":"image/png","size":1024}`)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		URL       string `json:"url"`
		Key       string `json:"key"`
		ExpiresIn int    `json:"expires_in"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Contains(t, out.URL, "X-Amz-Signature=")
	// Key is vendor-scoped under menu-images/<vendorID>/.
	assert.Contains(t, out.Key, "menu-images/"+vendorID+"/")
	assert.Equal(t, int(10*time.Minute/time.Second), out.ExpiresIn)
}

// --- presign download success (presign.go PresignedGet path) ---

func TestPresignDownload_OK(t *testing.T) {
	srv := buildPresign(t, employeeUser(), newS3(t, "http://127.0.0.1:9"))
	resp := do(t, http.MethodGet,
		srv.URL+"/api/menu/uploads/presigned?key=menu-images/v1/a.jpg", "")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		URL       string `json:"url"`
		Key       string `json:"key"`
		ExpiresIn int    `json:"expires_in"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Contains(t, out.URL, "X-Amz-Signature=")
	assert.Equal(t, "menu-images/v1/a.jpg", out.Key)
	assert.Equal(t, int(10*time.Minute/time.Second), out.ExpiresIn)
}

// --- HandleDirectUpload success + FormFile-missing + PutObject error ---------

func uploadAPI(t *testing.T, endpoint string) *mhttp.API {
	t.Helper()
	return &mhttp.API{
		Svc:                  &menu.Service{},
		Storage:              newS3(t, endpoint),
		StoragePublicBaseURL: "http://minio.tbite.local",
		StorageBucket:        "tbite-test",
	}
}

func directUploadReq(t *testing.T, body *bytes.Buffer, contentType string, u *identity.User) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/merchant/uploads", body)
	req = req.WithContext(idhttp.ContextWithUser(context.Background(), u))
	req.Header.Set("Content-Type", contentType)
	return req
}

// imageFormBody builds a valid multipart body with a "file" part carrying an
// explicit image content-type so validateImageUpload accepts it.
func imageFormBody(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := make(map[string][]string)
	h["Content-Disposition"] = []string{`form-data; name="file"; filename="photo.jpg"`}
	h["Content-Type"] = []string{"image/jpeg"}
	part, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, _ = part.Write([]byte("fake-jpeg-bytes"))
	require.NoError(t, mw.Close())
	return &buf, mw.FormDataContentType()
}

// NOTE: the PutObject success path (upload.go:80-83 — PublicURL + 200 JSON) is
// NOT exercised here. HandleDirectUpload streams an unseekable io.LimitReader to
// S3, which forces aws-chunked trailing-checksum signing; an in-process httptest
// server cannot satisfy that protocol (the SDK retries and fails to rewind), so
// a real S3-compatible backend (MinIO/testcontainers) is required. See the
// exemption note in the structured report.

func TestHandleDirectUpload_PutObjectError_500(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	a := uploadAPI(t, bad.URL)
	v := vendorID
	body, ct := imageFormBody(t)
	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, directUploadReq(t, body, ct,
		&identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "upload failed")
}

// TestHandleDirectUpload_FormFileMissing exercises the FormFile error branch:
// the multipart form parses successfully but has no "file" field (only a "note"
// text field), so r.FormFile("file") fails after ParseMultipartForm succeeds.
func TestHandleDirectUpload_FormFileMissing(t *testing.T) {
	a := uploadAPI(t, "http://127.0.0.1:9")

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("note", "no file here"))
	require.NoError(t, mw.Close())

	v := vendorID
	rec := httptest.NewRecorder()
	a.HandleDirectUpload(rec, directUploadReq(t, &buf, mw.FormDataContentType(),
		&identity.User{ID: "u-1", Role: identity.RoleVendorOperator, VendorID: &v}))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing file field")
}

// --- home_handler error branches reachable only via computeHome / cache ------

// buildHomeWithCache wires a HomeAPI with a pre-seeded cache so computeHome
// returns whatever HomeState was stored (used to inject a malformed TargetDay).
func buildHomeWithCache(t *testing.T, user *identity.User, seed map[string][]byte) *httptest.Server {
	t.Helper()
	home := &menu.HomeService{
		Clock:         clock.FixedClock{T: time.Date(2026, 5, 14, 8, 0, 0, 0, time.UTC)},
		ServerTZ:      time.UTC,
		RecentOrders:  &fakeRecentOrders{},
		Popularity:    &fakePopularity{},
		Affinity:      &fakeAffinity{},
		FavoritesRepo: &fakeFavForHome{},
	}
	menuSvc := &menu.Service{
		Categories: &fakeCategoryRepo{},
		Items:      &fakeItemRepo{byID: map[string]*menu.Item{}},
		Images:     &fakeImageRepo{byItem: map[string][]*menu.Image{}},
	}
	api := &mhttp.HomeAPI{Home: home, MenuSvc: menuSvc, Cache: &fakeCache{store: seed}}

	r := chi.NewRouter()
	if user != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req.WithContext(idhttp.ContextWithUser(req.Context(), user)))
			})
		})
	}
	h := humachi.New(r, huma.DefaultConfig("test", "0.0.0"))
	api.Register(h)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestGetHome_BadTargetDay_500 seeds the read-model cache with a HomeState whose
// TargetDay is not YYYY-MM-DD, so getHome's time.Parse(state.TargetDay) fails →
// 500 (home_handler.go:166-169).
func TestGetHome_BadTargetDay_500(t *testing.T) {
	// Cache key format: "home:<userID>:<plant>:<dayOverride>"; dayOverride empty.
	state, err := json.Marshal(map[string]any{"TargetDay": "not-a-date"})
	require.NoError(t, err)
	seed := map[string][]byte{"home:e-1:" + plant + ":": state}

	srv := buildHomeWithCache(t, employeeUser(), seed)
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/home", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// TestListReorders_ComputeError_500 makes Home.Compute fail (GetOrderByUserDate
// error) so listReorders' computeHome returns an error → 500
// (home_handler.go:235-237).
func TestListReorders_ComputeError_500(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.getErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/reorders", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// TestListRecommendations_ComputeError_500 omits ?day= so listRecommendations
// derives target_day via computeHome, which errors → 500
// (home_handler.go:267-269).
func TestListRecommendations_ComputeError_500(t *testing.T) {
	srv, ro, _, _, _, _ := buildHome(t, employeeUser())
	ro.getErr = errors.New("db down")
	resp := do(t, http.MethodGet, srv.URL+"/api/employee/recommendations", "")
	defer resp.Body.Close()
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
