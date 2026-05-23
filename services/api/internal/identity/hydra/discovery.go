package hydra

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// ReverseProxy returns a chi-compatible handler that forwards everything
// under the given path prefix to Hydra. We use it to expose Hydra's OAuth2
// surface (/oauth2/auth, /oauth2/token, /oauth2/register, /.well-known/...)
// through our public host so MCP clients only need to know one origin.
// Setting URLS_SELF_ISSUER on Hydra to our host makes the iss claim in
// access tokens line up with what clients fetch from discovery.
func ReverseProxy(hydraURL string) (http.Handler, error) {
	u, err := url.Parse(hydraURL)
	if err != nil {
		return nil, fmt.Errorf("parse hydra url %q: %w", hydraURL, err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	// Preserve the original Host header so Hydra's internal URL resolver
	// stays predictable. Hydra otherwise echoes the Host header back as
	// the OAuth issuer, which would re-introduce the mismatch we just
	// eliminated.
	director := rp.Director
	rp.Director = func(r *http.Request) {
		director(r)
		r.Host = u.Host
	}
	return rp, nil
}

// DiscoveryShim wraps Hydra's /.well-known/openid-configuration document
// and republishes it with a `registration_endpoint` field added so MCP
// clients (Claude.ai, ChatGPT) can complete RFC 7591 Dynamic Client
// Registration.
//
// Hydra v2.2 has the DCR endpoint live at /oauth2/register but does NOT
// advertise it in the discovery document — see
// https://github.com/ory/hydra/issues/4060 and the discussion in
// https://getlarge.eu/blog/securing-mcp-servers-with-oauth2-ory-hydra-claude-code-chatgpt/.
// Until Hydra ships the fix, we proxy the doc ourselves and patch it.
//
// The shim also doubles as the OAuth 2.0 Authorization Server Metadata
// document (RFC 8414) at /.well-known/oauth-authorization-server because
// MCP clients fall back to that URL if openid-configuration is missing.
type DiscoveryShim struct {
	// HydraURL is the URL the API process uses to reach Hydra (e.g.
	// http://hydra:4444 inside docker, http://localhost:4444 from the host).
	// This is where the upstream discovery doc and DCR endpoint actually live.
	HydraURL string

	// PublicBaseURL is the externally-facing T-Bite host (e.g.
	// https://api.tbite.com or http://localhost:8080). When set, the
	// patched discovery doc advertises registration_endpoint under this
	// host — which is where our reverse proxy receives /oauth2/register
	// and forwards it to Hydra. When empty we fall back to HydraURL,
	// which is correct for tests that don't run a proxy.
	PublicBaseURL string

	HTTP *http.Client

	mu       sync.RWMutex
	cached   json.RawMessage
	cachedAt time.Time
	cacheTTL time.Duration
}

// NewDiscoveryShim returns a shim that fetches + patches Hydra's discovery
// document with a 60-second cache. Hydra's doc rarely changes outside boot
// so a short cache greatly reduces upstream load when many MCP clients
// connect at once.
func NewDiscoveryShim(hydraURL string) *DiscoveryShim {
	return &DiscoveryShim{
		HydraURL: hydraURL,
		HTTP:     &http.Client{Timeout: 5 * time.Second},
		cacheTTL: 60 * time.Second,
	}
}

// ServeHTTP makes DiscoveryShim usable as a chi/http.Handler. Always emits
// JSON, falls back to a stub on upstream failure so MCP clients see
// well-formed JSON even when Hydra is briefly unreachable.
func (d *DiscoveryShim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	doc, err := d.Doc(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"hydra unavailable"}`))
		return
	}
	_, _ = w.Write(doc)
}

// Doc returns the patched discovery document. Cached for cacheTTL.
func (d *DiscoveryShim) Doc(ctx context.Context) (json.RawMessage, error) {
	d.mu.RLock()
	if d.cached != nil && time.Since(d.cachedAt) < d.cacheTTL {
		out := d.cached
		d.mu.RUnlock()
		return out, nil
	}
	d.mu.RUnlock()

	d.mu.Lock()
	defer d.mu.Unlock()
	// Double-check after re-acquiring lock.
	if d.cached != nil && time.Since(d.cachedAt) < d.cacheTTL {
		return d.cached, nil
	}

	req, err := http.NewRequestWithContext(ctx,
		http.MethodGet,
		d.HydraURL+"/.well-known/openid-configuration",
		nil,
	)
	if err != nil {
		return nil, err
	}
	resp, err := d.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hydra discovery fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("hydra discovery: %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Patch the document. Using map[string]any preserves Hydra's other
	// fields verbatim — important because clients may rely on niche fields
	// we don't know about.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("hydra discovery decode: %w", err)
	}
	// Advertise the registration endpoint under our public host when set
	// so the URL points at the reverse-proxy mount rather than Hydra
	// direct. MCP clients copy this URL verbatim into their DCR call.
	regHost := d.PublicBaseURL
	if regHost == "" {
		regHost = d.HydraURL
	}
	doc["registration_endpoint"] = regHost + "/oauth2/register"
	// MCP clients expect S256 PKCE to be supported (it always is on Hydra
	// but isn't always advertised in older docs).
	if _, ok := doc["code_challenge_methods_supported"]; !ok {
		doc["code_challenge_methods_supported"] = []string{"S256"}
	}

	patched, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	d.cached = patched
	d.cachedAt = time.Now()
	return patched, nil
}
