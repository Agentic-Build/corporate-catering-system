// Package hydra provides the integration between T-Bite and an Ory Hydra
// sidecar. Hydra fronts the OAuth 2.1 surface (authorize / token / register /
// jwks) so MCP clients with mandatory Dynamic Client Registration support
// (Claude.ai web, ChatGPT Custom Connectors) can self-register against us.
// User authentication itself is still handled by Authentik via the regular
// T-Bite OIDC login flow; this package only bridges Hydra's login/consent
// challenges into our existing session store.
package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AdminClient is a minimal HTTP client for Hydra's admin API (four endpoints
// the login + consent bridge needs; avoids the full Ory SDK dependency tree).
type AdminClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewAdminClient returns a client pointed at Hydra's admin endpoint. The admin
// port must never be reachable from the public internet.
func NewAdminClient(baseURL string) *AdminClient {
	return &AdminClient{
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// LoginRequest is the slice of Hydra's GetLoginRequest response we consume
// to decide between re-login and skip-to-consent.
type LoginRequest struct {
	Challenge      string   `json:"challenge"`
	Skip           bool     `json:"skip"`
	Subject        string   `json:"subject"`
	RequestURL     string   `json:"request_url"`
	RequestedScope []string `json:"requested_scope"`
	Client         struct {
		ClientID   string `json:"client_id"`
		ClientName string `json:"client_name"`
	} `json:"client"`
}

// ConsentRequest is the matching slice of GetConsentRequest. Skip is true
// when Hydra recognises the same client+subject pair as already consented.
type ConsentRequest struct {
	Challenge      string   `json:"challenge"`
	Skip           bool     `json:"skip"`
	Subject        string   `json:"subject"`
	RequestedScope []string `json:"requested_scope"`
	Client         struct {
		ClientID   string `json:"client_id"`
		ClientName string `json:"client_name"`
	} `json:"client"`
}

// AcceptLoginRequest accepts a login challenge with the given subject
// (identity.User.ID), returning the URL Hydra wants the browser redirected to.
func (c *AdminClient) AcceptLoginRequest(ctx context.Context, challenge, subject string) (string, error) {
	body := map[string]any{
		"subject":      subject,
		"remember":     true,
		"remember_for": 3600, // seconds — matches session TTL
	}
	return c.acceptChallenge(ctx, "/admin/oauth2/auth/requests/login/accept", challenge, body)
}

// GetLoginRequest fetches Hydra's view of a login challenge (skip=true →
// short-circuit; otherwise run the full Authentik flow).
func (c *AdminClient) GetLoginRequest(ctx context.Context, challenge string) (*LoginRequest, error) {
	var out LoginRequest
	if err := c.get(ctx, "/admin/oauth2/auth/requests/login", challenge, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AcceptConsentRequest accepts a consent challenge with the given scopes,
// returning Hydra's redirect URL. Claims are forwarded into id_token + access_token.
func (c *AdminClient) AcceptConsentRequest(ctx context.Context, challenge string, scopes []string, idTokenClaims map[string]any) (string, error) {
	body := map[string]any{
		"grant_scope": scopes,
		"session": map[string]any{
			"id_token":     idTokenClaims,
			"access_token": idTokenClaims,
		},
		"remember":     true,
		"remember_for": 3600,
	}
	return c.acceptChallenge(ctx, "/admin/oauth2/auth/requests/consent/accept", challenge, body)
}

// GetConsentRequest fetches Hydra's consent challenge details — used to
// confirm the requested scopes before auto-approving.
func (c *AdminClient) GetConsentRequest(ctx context.Context, challenge string) (*ConsentRequest, error) {
	var out ConsentRequest
	if err := c.get(ctx, "/admin/oauth2/auth/requests/consent", challenge, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *AdminClient) get(ctx context.Context, path, challenge string, dest any) error {
	u := c.BaseURL + path + "?" + url.Values{
		paramFor(path): []string{challenge},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("hydra admin GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("hydra admin GET %s: %d %s", path, resp.StatusCode, raw)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func (c *AdminClient) acceptChallenge(ctx context.Context, path, challenge string, body map[string]any) (string, error) {
	u := c.BaseURL + path + "?" + url.Values{
		paramFor(path): []string{challenge},
	}.Encode()
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("hydra admin PUT %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("hydra admin PUT %s: %d %s", path, resp.StatusCode, raw)
	}
	var out struct {
		RedirectTo string `json:"redirect_to"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if out.RedirectTo == "" {
		return "", errors.New("hydra accept response missing redirect_to")
	}
	return out.RedirectTo, nil
}

// paramFor returns the query-string key Hydra expects per admin endpoint
// (login_challenge / consent_challenge — Hydra is strict about the name).
func paramFor(path string) string {
	switch {
	case strings.Contains(path, "login"):
		return "login_challenge"
	case strings.Contains(path, "consent"):
		return "consent_challenge"
	default:
		return "challenge"
	}
}
