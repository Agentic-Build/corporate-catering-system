package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	idhttp "github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/http"
)

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	healthHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("content-type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestReadyHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()

	readyHandler(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("content-type"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "ready", body["status"])
}

func TestDrainHandlerMarksReadinessUnready(t *testing.T) {
	h := NewHealth()

	drainReq := httptest.NewRequest(http.MethodGet, "/drainz", nil)
	drainRR := httptest.NewRecorder()
	h.DrainHandler(0)(drainRR, drainReq)

	assert.Equal(t, http.StatusOK, drainRR.Code)

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRR := httptest.NewRecorder()
	h.ReadinessHandler()(readyRR, readyReq)

	assert.Equal(t, http.StatusServiceUnavailable, readyRR.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(readyRR.Body.Bytes(), &body))
	assert.Equal(t, "draining", body["status"])
}

func TestNewWithHealthUsesDependencyReadiness(t *testing.T) {
	h := NewHealth(CheckerFunc{
		N: "valkey",
		F: func(_ context.Context) error {
			return errors.New("connection refused")
		},
	})
	srv := NewWithHealth(":0", slog.New(slog.NewTextHandler(io.Discard, nil)), &idhttp.API{}, h, nil, MCP{})

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)

	var body struct {
		Status string `json:"status"`
		Deps   []struct {
			Name  string `json:"name"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"deps"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &body))
	assert.Equal(t, "not_ready", body.Status)
	require.Len(t, body.Deps, 1)
	assert.Equal(t, "valkey", body.Deps[0].Name)
	assert.False(t, body.Deps[0].OK)
	assert.Equal(t, "connection refused", body.Deps[0].Error)
}
