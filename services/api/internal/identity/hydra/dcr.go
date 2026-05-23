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

// SanitizingDCRProxy wraps the /oauth2/register endpoint to fix a known
// interoperability bug between Ory Hydra v2.2/v2.3 and strict OAuth clients
// (Claude Code, Claude.ai web, ChatGPT): Hydra emits optional URI fields
// (policy_uri, tos_uri, client_uri, logo_uri) as empty strings rather than
// omitting them, which fails RFC 8259/3986 URI validation in those clients.
//
// RFC 7591 §2 explicitly defines those fields as optional URIs, so emitting
// them as empty strings is wrong on Hydra's side. We forward the request
// untouched, then scrub the response before passing it back.
type SanitizingDCRProxy struct {
	HydraURL string
	HTTP     *http.Client
}

// ServeHTTP forwards POST /oauth2/register (and PUT/GET/DELETE for the
// registration_client_uri lifecycle) to Hydra and sanitises the response.
func (p *SanitizingDCRProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Build upstream URL preserving path + query.
	upstream, err := url.Parse(p.HydraURL)
	if err != nil {
		http.Error(w, "hydra url parse: "+err.Error(), http.StatusBadGateway)
		return
	}
	upstream.Path = strings.TrimRight(upstream.Path, "/") + r.URL.Path
	upstream.RawQuery = r.URL.RawQuery

	// Buffer body so we can forward and (if needed) retry — DCR responses
	// are small so this is cheap.
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}

	// On POST/PUT we may rewrite the scope field — see expandRegistrationScope.
	if r.Method == http.MethodPost || r.Method == http.MethodPut {
		if expanded, changed := expandRegistrationScope(body); changed {
			body = expanded
		}
	}

	req, err := http.NewRequestWithContext(r.Context(), r.Method, upstream.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, "build upstream req: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Copy headers verbatim except Host — Hydra needs the original-looking
	// Host for its own URL resolver.
	for k, vs := range r.Header {
		if strings.EqualFold(k, "Host") {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Host = upstream.Host

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

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "read upstream: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Only attempt to scrub JSON 2xx responses; pass everything else
	// (errors, 4xx, content-type mismatches) through verbatim.
	scrubbed := raw
	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.HasPrefix(ct, "application/json") {
		if cleaned, err := sanitizeDCRResponse(raw); err == nil {
			scrubbed = cleaned
		}
	}

	// Copy response headers, recompute Content-Length after scrubbing.
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
}

// expandRegistrationScope rewrites the client's `scope` field in a DCR
// request to always include `offline_access`. Strict MCP clients like
// Claude Code register WITHOUT offline_access but later request it at
// authorize time to mint a refresh token. Hydra binds the granted scope
// set at registration, so without this rewrite the authorize step fails
// with `invalid_scope: The OAuth 2.0 Client is not allowed to request
// scope 'offline_access'`. We also preserve `openid` so id_tokens stay
// available, and dedupe any scopes the client already listed.
//
// Returns (rewritten body, true) when the body was actually changed.
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

// sanitizeDCRResponse strips fields that strict OAuth clients reject:
//   - Empty-string optional URIs (policy_uri / tos_uri / client_uri /
//     logo_uri / jwks_uri). RFC 7591 says omit-or-set, not allow empty.
//   - The `contacts: null` field — RFC 7591 says contacts is an array of
//     strings; null breaks decoders that type it as []string.
//   - The `audience: []` field when empty. Some clients reject empty
//     arrays in audience because the spec defines audience as having at
//     least one entry.
//
// Mutating only the fields above keeps every Hydra-specific extension
// (registration_access_token, *_lifespan, etc.) intact so token refresh
// and re-registration via registration_client_uri keep working.
func sanitizeDCRResponse(raw []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	emptyURIKeys := []string{"policy_uri", "tos_uri", "client_uri", "logo_uri", "jwks_uri"}
	for _, k := range emptyURIKeys {
		if v, ok := doc[k]; ok {
			if s, ok := v.(string); ok && s == "" {
				delete(doc, k)
			}
		}
	}

	if v, ok := doc["contacts"]; ok && v == nil {
		delete(doc, "contacts")
	}

	if v, ok := doc["audience"]; ok {
		if a, ok := v.([]any); ok && len(a) == 0 {
			delete(doc, "audience")
		}
	}

	// allowed_cors_origins similarly may be sent as empty array.
	if v, ok := doc["allowed_cors_origins"]; ok {
		if a, ok := v.([]any); ok && len(a) == 0 {
			delete(doc, "allowed_cors_origins")
		}
	}

	return json.Marshal(doc)
}
