package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient builds an S3Client pointed at the given fake endpoint. Using
// path-style + a static-credentials provider keeps the SDK from contacting the
// real AWS metadata/STS services, so every call lands on the httptest server.
func newTestClient(t *testing.T, endpoint, bucket string) *S3Client {
	t.Helper()
	c, err := NewS3(context.Background(), S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		AccessKeyID:     "test",
		SecretAccessKey: "secret",
		Bucket:          bucket,
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	return c
}

func TestNewS3DefaultsRegion(t *testing.T) {
	// Empty region should fall back to us-east-1 without error.
	c, err := NewS3(context.Background(), S3Config{
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		Bucket:          "b",
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if c.Bucket != "b" {
		t.Fatalf("Bucket = %q, want b", c.Bucket)
	}
	if c.s3 == nil {
		t.Fatal("expected non-nil underlying s3 client")
	}
}

func TestNewS3ConfigLoadError(t *testing.T) {
	// Pointing AWS_CA_BUNDLE at a missing file makes LoadDefaultConfig fail,
	// exercising the "aws config" error-wrap branch. t.Setenv restores it.
	t.Setenv("AWS_CA_BUNDLE", "/nonexistent/path/ca.pem")
	_, err := NewS3(context.Background(), S3Config{
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		Bucket:          "b",
	})
	if err == nil {
		t.Fatal("expected error when AWS config fails to load")
	}
	if !strings.Contains(err.Error(), "aws config") {
		t.Fatalf("error = %v, want wrapped with 'aws config'", err)
	}
}

func TestNewS3ExplicitRegionAndEndpoint(t *testing.T) {
	c, err := NewS3(context.Background(), S3Config{
		Endpoint:        "http://minio.local:9000",
		Region:          "ap-northeast-1",
		AccessKeyID:     "k",
		SecretAccessKey: "s",
		Bucket:          "imgs",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if c.Bucket != "imgs" {
		t.Fatalf("Bucket = %q, want imgs", c.Bucket)
	}
}

func TestEnsureBucketHeadSucceeds(t *testing.T) {
	// HeadBucket returns 200 → bucket exists, EnsureBucket short-circuits.
	var sawCreate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			sawCreate = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	if err := c.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket() error = %v", err)
	}
	if sawCreate {
		t.Fatal("EnsureBucket should not CreateBucket when HeadBucket succeeds")
	}
}

func TestEnsureBucketCreatesWhenMissing(t *testing.T) {
	// HeadBucket 404 → CreateBucket; CreateBucket 200 → success.
	var sawCreate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			sawCreate = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	if err := c.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket() error = %v", err)
	}
	if !sawCreate {
		t.Fatal("expected CreateBucket call when HeadBucket fails")
	}
}

func TestEnsureBucketAlreadyOwnedTreatedAsSuccess(t *testing.T) {
	// CreateBucket returns BucketAlreadyOwnedByYou → idempotent success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>BucketAlreadyOwnedByYou</Code><Message>owned</Message></Error>`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	if err := c.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket() with BucketAlreadyOwnedByYou error = %v", err)
	}
}

func TestEnsureBucketAlreadyExistsTreatedAsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>BucketAlreadyExists</Code><Message>exists</Message></Error>`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	if err := c.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket() with BucketAlreadyExists error = %v", err)
	}
}

func TestEnsureBucketCreateFailsReturnsError(t *testing.T) {
	// CreateBucket returns an unrelated error → propagated.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>nope</Message></Error>`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	err := c.EnsureBucket(context.Background())
	if err == nil {
		t.Fatal("expected error from EnsureBucket when CreateBucket fails")
	}
	if !strings.Contains(err.Error(), "create bucket") {
		t.Fatalf("error = %v, want wrapped with 'create bucket'", err)
	}
}

func TestCheckSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	if err := c.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	err := c.Check(context.Background())
	if err == nil {
		t.Fatal("expected error from Check when HeadBucket fails")
	}
	if !strings.Contains(err.Error(), "head bucket") {
		t.Fatalf("error = %v, want wrapped with 'head bucket'", err)
	}
}

func TestPutObjectSucceeds(t *testing.T) {
	var gotBody string
	var gotContentType string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	uri, err := c.PutObject(context.Background(), "imgs/a.png", strings.NewReader("hello"), "image/png")
	if err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if uri != "s3://bucket/imgs/a.png" {
		t.Fatalf("uri = %q, want s3://bucket/imgs/a.png", uri)
	}
	if gotBody != "hello" {
		t.Fatalf("uploaded body = %q, want hello", gotBody)
	}
	if gotContentType != "image/png" {
		t.Fatalf("content-type = %q, want image/png", gotContentType)
	}
	if !strings.Contains(gotPath, "imgs/a.png") {
		t.Fatalf("path = %q, want to contain imgs/a.png", gotPath)
	}
}

func TestPutObjectFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>no</Message></Error>`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	_, err := c.PutObject(context.Background(), "k", strings.NewReader("x"), "text/plain")
	if err == nil {
		t.Fatal("expected error from PutObject")
	}
	if !strings.Contains(err.Error(), "put k") {
		t.Fatalf("error = %v, want wrapped with 'put k'", err)
	}
}

func TestGetObjectSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "payload")
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	rc, err := c.GetObject(context.Background(), "k")
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "payload" {
		t.Fatalf("body = %q, want payload", b)
	}
}

func TestGetObjectFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>gone</Message></Error>`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, "bucket")
	_, err := c.GetObject(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error from GetObject")
	}
	if !strings.Contains(err.Error(), "get missing") {
		t.Fatalf("error = %v, want wrapped with 'get missing'", err)
	}
}

func TestPresignedPutDefaultsTTL(t *testing.T) {
	// Presigning is purely local (no network), so no server is needed.
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	// ttl <= 0 exercises the default-TTL branch.
	u, err := c.PresignedPut(context.Background(), "imgs/a.png", "image/png", 2_000_000, 0)
	if err != nil {
		t.Fatalf("PresignedPut() error = %v", err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("returned URL not parseable: %v", err)
	}
	if !strings.Contains(parsed.Path, "imgs/a.png") {
		t.Fatalf("presigned URL path = %q, want to contain imgs/a.png", parsed.Path)
	}
	if parsed.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("presigned URL missing X-Amz-Signature")
	}
	// Default TTL is 10 minutes → 600 seconds.
	if got := parsed.Query().Get("X-Amz-Expires"); got != "600" {
		t.Fatalf("X-Amz-Expires = %q, want 600 (default 10m)", got)
	}
}

func TestPresignedPutExplicitTTL(t *testing.T) {
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	u, err := c.PresignedPut(context.Background(), "k", "text/plain", 100, 30*time.Second)
	if err != nil {
		t.Fatalf("PresignedPut() error = %v", err)
	}
	parsed, _ := url.Parse(u)
	if got := parsed.Query().Get("X-Amz-Expires"); got != "30" {
		t.Fatalf("X-Amz-Expires = %q, want 30", got)
	}
}

func TestPresignedPutSerializationError(t *testing.T) {
	// An empty key fails request serialization inside the presigner, hitting
	// the error-wrap branch without any network call.
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	_, err := c.PresignedPut(context.Background(), "", "image/png", 100, time.Minute)
	if err == nil {
		t.Fatal("expected error from PresignedPut with empty key")
	}
	if !strings.Contains(err.Error(), "presign put") {
		t.Fatalf("error = %v, want wrapped with 'presign put'", err)
	}
}

func TestPresignedGetDefaultsTTL(t *testing.T) {
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	u, err := c.PresignedGet(context.Background(), "imgs/a.png", 0)
	if err != nil {
		t.Fatalf("PresignedGet() error = %v", err)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("returned URL not parseable: %v", err)
	}
	if parsed.Query().Get("X-Amz-Signature") == "" {
		t.Fatal("presigned URL missing X-Amz-Signature")
	}
	if got := parsed.Query().Get("X-Amz-Expires"); got != "600" {
		t.Fatalf("X-Amz-Expires = %q, want 600 (default 10m)", got)
	}
}

func TestPresignedGetSerializationError(t *testing.T) {
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	_, err := c.PresignedGet(context.Background(), "", time.Minute)
	if err == nil {
		t.Fatal("expected error from PresignedGet with empty key")
	}
	if !strings.Contains(err.Error(), "presign get") {
		t.Fatalf("error = %v, want wrapped with 'presign get'", err)
	}
}

func TestPresignedGetExplicitTTL(t *testing.T) {
	c := newTestClient(t, "http://minio.local:9000", "bucket")
	u, err := c.PresignedGet(context.Background(), "k", 45*time.Second)
	if err != nil {
		t.Fatalf("PresignedGet() error = %v", err)
	}
	parsed, _ := url.Parse(u)
	if got := parsed.Query().Get("X-Amz-Expires"); got != "45" {
		t.Fatalf("X-Amz-Expires = %q, want 45", got)
	}
}
