package hydra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// SanitizingDCRProxy wraps /oauth2/register to work around Ory Hydra v2.2/v2.3
// emitting optional URI fields as empty strings (policy_uri, tos_uri,
// client_uri, logo_uri) — strict OAuth clients (Claude Code/web, ChatGPT)
// reject these per RFC 7591 §2. We forward the request and scrub the response.
type SanitizingDCRProxy struct {
	HydraURL string
	HTTP     *http.Client
}

// buildUpstreamRequest reads the inbound body, expands the DCR scope when
// applicable, and returns the upstream request pointed at Hydra.
func (p *SanitizingDCRProxy) buildUpstreamRequest(r *http.Request) (*http.Request, error) {
	upstream, err := url.Parse(p.HydraURL)
	if err != nil {
		return nil, fmt.Errorf("hydra url parse: %w", err)
	}
	upstream.Path = strings.TrimRight(upstream.Path, "/") + r.URL.Path
	upstream.RawQuery = r.URL.RawQuery

	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if expanded, changed := expandRegistrationScope(body); changed {
			body = expanded
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build upstream req: %w", err)
	}
	for k, vs := range r.Header {
		if strings.EqualFold(k, "Host") {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Host = upstream.Host
	return req, nil
}

// writeSanitizedResponse forwards Hydra's response back to the client, scrubbing
// JSON 2xx bodies and recomputing Content-Length.
func writeSanitizedResponse(w http.ResponseWriter, resp *http.Response) error {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read upstream: %w", err)
	}
	scrubbed := raw
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(ct, "application/json") {
		if cleaned, err := sanitizeDCRResponse(raw); err == nil {
			scrubbed = cleaned
		}
	}
	for k, vs := range resp.Header {
		if strings.EqualFold(k, "Content-Length") {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(scrubbed)))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(scrubbed)
	return nil
}

// ServeHTTP forwards POST /oauth2/register (and PUT/GET/DELETE for the
// registration_client_uri lifecycle) to Hydra and sanitises the response.
func (p *SanitizingDCRProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, err := p.buildUpstreamRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	client := p.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "hydra DCR upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if err := writeSanitizedResponse(w, resp); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
}

// expandRegistrationScope adds `openid` + `offline_access` to a DCR request's
// scope. Strict MCP clients (Claude Code) register without offline_access but
// later request it at authorize, which Hydra rejects (scope is bound at
// registration). Returns (rewritten body, true) when changed.
func expandRegistrationScope(body []byte) ([]byte, bool) {
	if len(body) == 0 {
		return body, false
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return body, false
	}
	current, _ := doc["scope"].(string)
	scopes := strings.Fields(current)
	required := []string{"openid", "offline_access"}
	added := false
	for _, want := range required {
		found := false
		for _, have := range scopes {
			if have == want {
				found = true
				break
			}
		}
		if !found {
			scopes = append(scopes, want)
			added = true
		}
	}
	if !added {
		return body, false
	}
	doc["scope"] = strings.Join(scopes, " ")
	out, err := json.Marshal(doc)
	if err != nil {
		return body, false
	}
	return out, true
}

// stripEmptyURIs deletes the optional URI fields whose value is "".
func stripEmptyURIs(doc map[string]any) {
	for _, k := range []string{"policy_uri", "tos_uri", "client_uri", "logo_uri", "jwks_uri"} {
		if v, ok := doc[k]; ok {
			if s, ok := v.(string); ok && s == "" {
				delete(doc, k)
			}
		}
	}
}

// stripEmptyArrayKey deletes key when doc[key] is an empty []any.
func stripEmptyArrayKey(doc map[string]any, key string) {
	if v, ok := doc[key]; ok {
		if a, ok := v.([]any); ok && len(a) == 0 {
			delete(doc, key)
		}
	}
}

// sanitizeDCRResponse strips fields that strict OAuth clients reject:
// empty-string optional URIs (policy_uri/tos_uri/client_uri/logo_uri/jwks_uri),
// `contacts: null`, and empty `audience: []`/`allowed_cors_origins: []`. All
// Hydra-specific extensions (registration_access_token, *_lifespan, …) pass
// through so refresh/re-registration keep working.
func sanitizeDCRResponse(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	stripEmptyURIs(doc)
	if v, ok := doc["contacts"]; ok && v == nil {
		delete(doc, "contacts")
	}
	stripEmptyArrayKey(doc, "audience")
	stripEmptyArrayKey(doc, "allowed_cors_origins")
	return json.Marshal(doc)
}
