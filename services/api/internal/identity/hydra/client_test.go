package hydra_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity/hydra"
)

func TestNewAdminClient(t *testing.T) {
	c := hydra.NewAdminClient("http://hydra:4445")
	require.NotNil(t, c)
	assert.Equal(t, "http://hydra:4445", c.BaseURL)
	require.NotNil(t, c.HTTP)
}

func TestAdminClient_GetLoginRequest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/oauth2/auth/requests/login", r.URL.Path)
		assert.Equal(t, "chal-1", r.URL.Query().Get("login_challenge"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		_, _ = w.Write([]byte(`{"challenge":"chal-1","skip":true,"subject":"u1","requested_scope":["openid"]}`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.GetLoginRequest(context.Background(), "chal-1")
	require.NoError(t, err)
	assert.True(t, got.Skip)
	assert.Equal(t, "u1", got.Subject)
}

func TestAdminClient_GetLoginRequest_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`not found`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.GetLoginRequest(context.Background(), "chal-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestAdminClient_GetLoginRequest_TransportError(t *testing.T) {
	// Unreachable base URL → HTTP.Do returns an error.
	c := &hydra.AdminClient{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{}}
	_, err := c.GetLoginRequest(context.Background(), "chal-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydra admin GET")
}

func TestAdminClient_GetLoginRequest_BadRequestBuild(t *testing.T) {
	// A control character in BaseURL makes http.NewRequestWithContext fail.
	c := &hydra.AdminClient{BaseURL: "http://\x7f/", HTTP: &http.Client{}}
	_, err := c.GetLoginRequest(context.Background(), "chal-1")
	require.Error(t, err)
}

func TestAdminClient_GetConsentRequest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/oauth2/auth/requests/consent", r.URL.Path)
		assert.Equal(t, "cc-1", r.URL.Query().Get("consent_challenge"))
		_, _ = w.Write([]byte(`{"challenge":"cc-1","subject":"u2","requested_scope":["openid","offline_access"]}`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.GetConsentRequest(context.Background(), "cc-1")
	require.NoError(t, err)
	assert.Equal(t, "u2", got.Subject)
	assert.Equal(t, []string{"openid", "offline_access"}, got.RequestedScope)
}

func TestAdminClient_AcceptLoginRequest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/admin/oauth2/auth/requests/login/accept", r.URL.Path)
		assert.Equal(t, "lc-9", r.URL.Query().Get("login_challenge"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/continue"}`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	url, err := c.AcceptLoginRequest(context.Background(), "lc-9", "subject-x")
	require.NoError(t, err)
	assert.Equal(t, "https://hydra/continue", url)
}

func TestAdminClient_AcceptConsentRequest_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/admin/oauth2/auth/requests/consent/accept", r.URL.Path)
		assert.Equal(t, "cc-9", r.URL.Query().Get("consent_challenge"))
		_, _ = w.Write([]byte(`{"redirect_to":"https://hydra/done"}`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	url, err := c.AcceptConsentRequest(context.Background(), "cc-9",
		[]string{"openid"}, map[string]any{"email": "x@y.com"})
	require.NoError(t, err)
	assert.Equal(t, "https://hydra/done", url)
}

func TestAdminClient_AcceptChallenge_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`conflict`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.AcceptLoginRequest(context.Background(), "lc", "sub")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "409")
}

func TestAdminClient_AcceptChallenge_MissingRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.AcceptLoginRequest(context.Background(), "lc", "sub")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing redirect_to")
}

func TestAdminClient_AcceptChallenge_BadResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := &hydra.AdminClient{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.AcceptLoginRequest(context.Background(), "lc", "sub")
	require.Error(t, err)
}

func TestAdminClient_AcceptChallenge_TransportError(t *testing.T) {
	c := &hydra.AdminClient{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{}}
	_, err := c.AcceptConsentRequest(context.Background(), "cc", nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "hydra admin PUT")
}

func TestAdminClient_AcceptChallenge_BadRequestBuild(t *testing.T) {
	c := &hydra.AdminClient{BaseURL: "http://\x7f/", HTTP: &http.Client{}}
	_, err := c.AcceptLoginRequest(context.Background(), "lc", "sub")
	require.Error(t, err)
}

// json.Marshal of the request body fails when a claim value is unmarshalable
// (a channel). Exercises acceptChallenge's marshal-error return.
func TestAdminClient_AcceptConsent_MarshalError(t *testing.T) {
	c := &hydra.AdminClient{BaseURL: "http://hydra", HTTP: &http.Client{}}
	bad := map[string]any{"bad": make(chan int)}
	_, err := c.AcceptConsentRequest(context.Background(), "cc", []string{"openid"}, bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "json")
}
