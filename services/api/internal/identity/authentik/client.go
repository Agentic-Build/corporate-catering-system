package authentik

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
)

type Config struct {
	BaseURL             string
	APIToken            string
	Provider            string
	VendorOperatorGroup string
	HTTPClient          *http.Client
}

type Client struct {
	baseURL             *url.URL
	apiToken            string
	provider            string
	vendorOperatorGroup string
	httpClient          *http.Client
}

func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, errors.New("authentik: base url is required")
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("authentik: invalid base url %q", cfg.BaseURL)
	}
	if strings.TrimSpace(cfg.APIToken) == "" {
		return nil, errors.New("authentik: api token is required")
	}
	if strings.TrimSpace(cfg.Provider) == "" {
		return nil, errors.New("authentik: provider slug is required")
	}
	if strings.TrimSpace(cfg.VendorOperatorGroup) == "" {
		return nil, errors.New("authentik: vendor operator group is required")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:             u,
		apiToken:            cfg.APIToken,
		provider:            strings.TrimSpace(cfg.Provider),
		vendorOperatorGroup: strings.TrimSpace(cfg.VendorOperatorGroup),
		httpClient:          client,
	}, nil
}

func (c *Client) UpsertVendorOperator(ctx context.Context, in identity.VendorOperatorProvisionInput) (*identity.VendorOperatorProvisioned, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	displayName := strings.TrimSpace(in.DisplayName)
	if email == "" || displayName == "" || in.VendorID == "" {
		return nil, errors.New("authentik: email, display name, and vendor id are required")
	}
	group, err := c.groupByName(ctx, c.vendorOperatorGroup)
	if err != nil {
		return nil, err
	}
	user, err := c.userByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	attrs := map[string]any{}
	groups := []string{group.PK}
	if user != nil {
		attrs = user.Attributes
		groups = appendMissing(user.Groups, group.PK)
	}
	attrs["tbite_role"] = string(identity.RoleVendorOperator)
	attrs["tbite_vendor_id"] = in.VendorID
	userReq := userWriteRequest{
		Username:   email,
		Name:       displayName,
		Email:      email,
		IsActive:   in.Active,
		Groups:     groups,
		Attributes: attrs,
		Path:       "tbite/vendors/" + in.VendorID,
		Type:       "internal",
	}
	if user == nil {
		user, err = c.createUser(ctx, userReq)
	} else {
		user, err = c.patchUser(ctx, user.PK, userReq)
	}
	if err != nil {
		return nil, err
	}
	if user.UUID == "" {
		return nil, errors.New("authentik: user response missing uuid; provider subject_mode must be user_uuid")
	}
	recovery, err := c.recoveryLink(ctx, user.PK)
	if err != nil {
		return nil, err
	}
	return &identity.VendorOperatorProvisioned{
		Provider:        c.provider,
		ExternalSubject: user.UUID,
		SetupURL:        recovery.Link,
	}, nil
}

func (c *Client) SuspendVendorOperator(ctx context.Context, provider, externalSubject string) error {
	if provider != c.provider {
		return fmt.Errorf("authentik: unsupported provider %q", provider)
	}
	user, err := c.userByUUID(ctx, externalSubject)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("authentik: user not found")
	}
	_, err = c.patchUser(ctx, user.PK, userWriteRequest{
		IsActive:   false,
		Attributes: user.Attributes,
		Groups:     user.Groups,
	})
	return err
}

func (c *Client) ReinstateVendorOperator(ctx context.Context, provider, externalSubject, vendorID string) error {
	if provider != c.provider {
		return fmt.Errorf("authentik: unsupported provider %q", provider)
	}
	user, err := c.userByUUID(ctx, externalSubject)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("authentik: user not found")
	}
	group, err := c.groupByName(ctx, c.vendorOperatorGroup)
	if err != nil {
		return err
	}
	attrs := user.Attributes
	if attrs == nil {
		attrs = map[string]any{}
	}
	attrs["tbite_role"] = string(identity.RoleVendorOperator)
	attrs["tbite_vendor_id"] = vendorID
	_, err = c.patchUser(ctx, user.PK, userWriteRequest{
		IsActive:   true,
		Groups:     appendMissing(user.Groups, group.PK),
		Attributes: attrs,
	})
	return err
}

func (c *Client) userByEmail(ctx context.Context, email string) (*userResponse, error) {
	values := url.Values{"email": {email}, "page_size": {"1"}}
	var out listResponse[userResponse]
	if err := c.do(ctx, http.MethodGet, "/api/v3/core/users/?"+values.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	return &out.Results[0], nil
}

func (c *Client) userByUUID(ctx context.Context, uuid string) (*userResponse, error) {
	values := url.Values{"uuid": {uuid}, "page_size": {"1"}}
	var out listResponse[userResponse]
	if err := c.do(ctx, http.MethodGet, "/api/v3/core/users/?"+values.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, nil
	}
	return &out.Results[0], nil
}

func (c *Client) groupByName(ctx context.Context, name string) (*groupResponse, error) {
	values := url.Values{"name": {name}, "page_size": {"1"}, "include_users": {"false"}}
	var out listResponse[groupResponse]
	if err := c.do(ctx, http.MethodGet, "/api/v3/core/groups/?"+values.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if len(out.Results) == 0 {
		return nil, fmt.Errorf("authentik: group %q not found", name)
	}
	for _, group := range out.Results {
		if group.Name == name {
			return &group, nil
		}
	}
	return nil, fmt.Errorf("authentik: group %q not found", name)
}

func (c *Client) createUser(ctx context.Context, in userWriteRequest) (*userResponse, error) {
	var out userResponse
	if err := c.do(ctx, http.MethodPost, "/api/v3/core/users/", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) patchUser(ctx context.Context, pk int, in userWriteRequest) (*userResponse, error) {
	var out userResponse
	if err := c.do(ctx, http.MethodPatch, fmt.Sprintf("/api/v3/core/users/%d/", pk), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) recoveryLink(ctx context.Context, pk int) (*recoveryResponse, error) {
	var out recoveryResponse
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/api/v3/core/users/%d/recovery/", pk), nil, &out); err != nil {
		return nil, err
	}
	if out.Link == "" {
		return nil, errors.New("authentik: recovery response missing link")
	}
	return &out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	u := c.baseURL.ResolveReference(&url.URL{Path: path})
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		u = c.baseURL.ResolveReference(&url.URL{Path: parts[0], RawQuery: parts[1]})
	}
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("authentik: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("authentik: request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("authentik: %s %s returned %s", method, path, resp.Status)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("authentik: decode response: %w", err)
	}
	return nil
}

type listResponse[T any] struct {
	Results []T `json:"results"`
}

type userResponse struct {
	PK         int            `json:"pk"`
	UUID       string         `json:"uuid"`
	Username   string         `json:"username"`
	Name       string         `json:"name"`
	Email      string         `json:"email"`
	IsActive   bool           `json:"is_active"`
	Groups     []string       `json:"groups"`
	Attributes map[string]any `json:"attributes"`
}

type groupResponse struct {
	PK   string `json:"pk"`
	Name string `json:"name"`
}

type userWriteRequest struct {
	Username   string         `json:"username,omitempty"`
	Name       string         `json:"name,omitempty"`
	Email      string         `json:"email,omitempty"`
	IsActive   bool           `json:"is_active"`
	Groups     []string       `json:"groups,omitempty"`
	Attributes map[string]any `json:"attributes,omitempty"`
	Path       string         `json:"path,omitempty"`
	Type       string         `json:"type,omitempty"`
}

type recoveryResponse struct {
	Link string `json:"link"`
}

func appendMissing(items []string, item string) []string {
	if slices.Contains(items, item) {
		return items
	}
	out := append([]string{}, items...)
	return append(out, item)
}
