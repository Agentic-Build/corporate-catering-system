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

// ReverseProxy forwards Hydra's OAuth2 surface (/oauth2/*, /.well-known/*)
// through our public host so MCP clients only need to know one origin. Pair
// with URLS_SELF_ISSUER so the JWT iss claim matches what clients discover.
func ReverseProxy(hydraURL string) (http.Handler, error) {
	u, err := url.Parse(hydraURL)
	if err != nil {
		return nil, fmt.Errorf("parse hydra url %q: %w", hydraURL, err)
	}
	rp := httputil.NewSingleHostReverseProxy(u)
	// Force Host = upstream host so Hydra doesn't echo our host back as the
	// OAuth issuer (would re-introduce the mismatch we just eliminated).
	director := rp.Director
	rp.Director = func(r *http.Request) {
		director(r)
		r.Host = u.Host
	}
	return rp, nil
}

// DiscoveryShim wraps Hydra's /.well-known/openid-configuration and patches
// it with registration_endpoint so MCP clients (Claude.ai, ChatGPT) can do
// RFC 7591 DCR. Hydra v2.2 hosts /oauth2/register but doesn't advertise it
// (https://github.com/ory/hydra/issues/4060). Also doubles as the RFC 8414
// authorization-server-metadata document.
type DiscoveryShim struct {
	// HydraURL is the URL the API process uses to reach Hydra.
	HydraURL string

	// PublicBaseURL is the externally-facing T-Bite host. When set, the patched
	// discovery doc advertises registration_endpoint under it; clients then hit
	// our reverse proxy. Empty → fall back to HydraURL (for tests).
	PublicBaseURL string

	HTTP *http.Client

	mu       sync.RWMutex
	cached   json.RawMessage
	cachedAt time.Time
	cacheTTL time.Duration
}

// NewDiscoveryShim returns a shim with a 60-second cache.
func NewDiscoveryShim(hydraURL string) *DiscoveryShim {
	return &DiscoveryShim{
		HydraURL: hydraURL,
		HTTP:     &http.Client{Timeout: 5 * time.Second},
		cacheTTL: 60 * time.Second,
	}
}

// ServeHTTP makes DiscoveryShim usable as an http.Handler. Always emits JSON;
// returns a well-formed stub on upstream failure.
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

	// Patch the document; map[string]any preserves other fields verbatim.
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("hydra discovery decode: %w", err)
	}
	// Advertise registration_endpoint under our public host so DCR hits the
	// reverse proxy, not Hydra directly.
	regHost := d.PublicBaseURL
	if regHost == "" {
		regHost = d.HydraURL
	}
	doc["registration_endpoint"] = regHost + "/oauth2/register"
	// MCP clients expect S256 PKCE advertised (Hydra supports it but older
	// docs don't always list it).
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
