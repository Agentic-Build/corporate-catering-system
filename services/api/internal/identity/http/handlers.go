package idhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
	"github.com/takalawang/corporate-catering-system/services/api/internal/identity/oidc"
)

// AppBaseURLs maps app names ("employee"|"merchant"|"admin") to their public base URLs.
type AppBaseURLs map[string]string

type API struct {
	Svc      *identity.Service
	Sessions identity.SessionStore
	Users    identity.UserRepository
	AppURLs  AppBaseURLs

	// Handoff brokers the single-use login code so the session token is never
	// placed in the callback redirect URL. When nil, completeLogin falls back
	// to the legacy token-in-URL behaviour.
	Handoff identity.AuthHandoffStore

	// JWT, when non-nil, lets AuthMiddleware accept JWT bearer tokens
	// issued by Hydra in addition to T-Bite session tokens. The api role
	// wires this; mcp-stdio leaves it nil because it speaks the
	// session-token model only.
	JWT JWTVerifier
}

// ----- DTOs -----

type startLoginInput struct {
	Provider string `path:"provider" doc:"OIDC provider slug"`
	Body     struct {
		App      string `json:"app" enum:"employee,merchant,admin"`
		ReturnTo string `json:"return_to,omitempty"`
	}
}
type startLoginOutput struct {
	Body struct {
		AuthURL string `json:"auth_url"`
		State   string `json:"state"`
	}
}

type completeLoginInput struct {
	Provider string `path:"provider"`
	State    string `query:"state"`
	Code     string `query:"code"`
}
type completeLoginOutput struct {
	Status int
	Url    string `header:"Location"`
}

type meOutput struct {
	Body struct {
		UserID      string  `json:"user_id"`
		Email       string  `json:"email"`
		DisplayName string  `json:"display_name"`
		Role        string  `json:"role"`
		EmployeeID  *string `json:"employee_id,omitempty"`
		Plant       *string `json:"plant,omitempty"`
		Department  *string `json:"department,omitempty"`
		VendorID    *string `json:"vendor_id,omitempty"`
	}
}

type providersOutput struct {
	Body struct {
		Items []providerDTO `json:"items"`
	}
}

type providerDTO struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
}

// ----- Registration -----

func (a *API) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "listAuthProviders",
		Method:      http.MethodGet,
		Path:        "/auth/providers",
		Summary:     "List enabled auth providers",
		Tags:        []string{"auth"},
	}, a.providers)

	huma.Register(api, huma.Operation{
		OperationID: "startLogin",
		Method:      http.MethodPost,
		Path:        "/auth/{provider}/start",
		Summary:     "Start OIDC login",
		Tags:        []string{"auth"},
	}, a.startLogin)

	huma.Register(api, huma.Operation{
		OperationID: "completeLogin",
		Method:      http.MethodGet,
		Path:        "/auth/{provider}/callback",
		Summary:     "Complete OIDC login (redirects to app landing)",
		Tags:        []string{"auth"},
	}, a.completeLogin)

	huma.Register(api, huma.Operation{
		OperationID: "exchangeAuthSession",
		Method:      http.MethodPost,
		Path:        "/auth/session",
		Summary:     "Exchange a single-use login code for a session token",
		Tags:        []string{"auth"},
	}, a.exchangeSession)

	huma.Register(api, huma.Operation{
		OperationID:   "logout",
		Method:        http.MethodPost,
		Path:          "/auth/logout",
		Summary:       "Revoke current session",
		Tags:          []string{"auth"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: http.StatusNoContent,
	}, a.logout)

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/me",
		Summary:     "Get current user",
		Tags:        []string{"auth"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, a.me)
}

func (a *API) startLogin(ctx context.Context, in *startLoginInput) (*startLoginOutput, error) {
	out, err := a.Svc.StartLogin(ctx, identity.StartLoginInput{
		App: in.Body.App, Provider: in.Provider, ReturnTo: in.Body.ReturnTo,
	})
	if err != nil {
		return nil, mapErr(err)
	}
	var resp startLoginOutput
	resp.Body.AuthURL = out.AuthURL
	resp.Body.State = out.State
	return &resp, nil
}

func (a *API) completeLogin(ctx context.Context, in *completeLoginInput) (*completeLoginOutput, error) {
	out, err := a.Svc.CompleteLogin(ctx, identity.CompleteLoginInput{
		Provider: in.Provider, State: in.State, Code: in.Code,
	})
	if err != nil {
		// This is a browser-facing callback: render a friendly redirect back
		// to the app's login page rather than returning raw JSON. We can only
		// do so once we know which app the user came from, which the service
		// reports via CallbackError after the OIDC state is resolved.
		var cbErr *identity.CallbackError
		if errors.As(err, &cbErr) {
			if base, ok := a.AppURLs[cbErr.App]; ok {
				login := fmt.Sprintf("%s/login?error=%s",
					base, url.QueryEscape(callbackErrorCode(err)))
				return &completeLoginOutput{Status: http.StatusSeeOther, Url: login}, nil
			}
		}
		return nil, mapErr(err)
	}

	base, ok := a.AppURLs[out.App]
	if !ok {
		return nil, huma.Error500InternalServerError("unknown app base url for " + out.App)
	}
	// Hand the session token to the app via a single-use code rather than the
	// URL, so it never lands in browser history or proxy logs. The app server
	// redeems the code at POST /auth/session over its server-to-API channel.
	if a.Handoff != nil {
		code, err := a.Handoff.IssueCode(ctx, out.Session.Token)
		if err != nil {
			return nil, huma.Error500InternalServerError("issue auth handoff", err)
		}
		landing := fmt.Sprintf("%s/auth/landing?code=%s&return_to=%s",
			base, url.QueryEscape(code), url.QueryEscape(out.ReturnTo))
		return &completeLoginOutput{Status: http.StatusFound, Url: landing}, nil
	}
	landing := fmt.Sprintf("%s/auth/landing?token=%s&return_to=%s",
		base,
		url.QueryEscape(out.Session.Token),
		url.QueryEscape(out.ReturnTo),
	)
	return &completeLoginOutput{Status: http.StatusFound, Url: landing}, nil
}

type exchangeSessionInput struct {
	Body struct {
		Code string `json:"code" doc:"Single-use login code from the callback redirect"`
	}
}
type exchangeSessionOutput struct {
	Body struct {
		Token string `json:"token"`
	}
}

// exchangeSession redeems a single-use login code for its session token. It is
// unauthenticated by design — possession of the short-lived, single-use code
// is the credential (OAuth authorization-code style). Called server-side by
// the app's /auth/landing endpoint.
func (a *API) exchangeSession(ctx context.Context, in *exchangeSessionInput) (*exchangeSessionOutput, error) {
	if a.Handoff == nil {
		return nil, huma.Error503ServiceUnavailable("auth handoff not configured")
	}
	if in.Body.Code == "" {
		return nil, huma.Error400BadRequest("code required")
	}
	token, err := a.Handoff.RedeemCode(ctx, in.Body.Code)
	if err != nil {
		return nil, huma.Error401Unauthorized("invalid or expired code")
	}
	var resp exchangeSessionOutput
	resp.Body.Token = token
	return &resp, nil
}

// callbackErrorCode maps an OIDC callback failure to a short, stable code the
// app's /login page can use to render a friendly message (no raw detail leaks).
func callbackErrorCode(err error) string {
	switch {
	case errors.Is(err, identity.ErrRoleMismatch):
		return "role_mismatch"
	case errors.Is(err, identity.ErrAccountSuspended):
		return "account_suspended"
	case errors.Is(err, oidc.ErrStateConsumed),
		errors.Is(err, oidc.ErrStateNotFound):
		return "auth_expired"
	default:
		return "auth_failed"
	}
}

func (a *API) providers(_ context.Context, _ *struct{}) (*providersOutput, error) {
	infos := a.Svc.ProviderInfos()
	resp := providersOutput{}
	resp.Body.Items = make([]providerDTO, 0, len(infos))
	for _, p := range infos {
		resp.Body.Items = append(resp.Body.Items, providerDTO{
			Slug:        p.Slug,
			DisplayName: p.DisplayName,
		})
	}
	return &resp, nil
}

func (a *API) logout(ctx context.Context, _ *struct{}) (*struct{}, error) {
	tok, ok := TokenFromContext(ctx)
	if !ok || tok == "" {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	if err := a.Sessions.Revoke(ctx, tok); err != nil {
		return nil, huma.Error500InternalServerError("revoke failed", err)
	}
	return &struct{}{}, nil
}

func (a *API) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	u, ok := UserFromContext(ctx)
	if !ok {
		return nil, huma.Error401Unauthorized("not authenticated")
	}
	var resp meOutput
	resp.Body.UserID = u.ID
	resp.Body.Email = u.PrimaryEmail
	resp.Body.DisplayName = u.DisplayName
	resp.Body.Role = string(u.Role)
	resp.Body.EmployeeID = u.EmployeeID
	resp.Body.Plant = u.Plant
	resp.Body.Department = u.Department
	resp.Body.VendorID = u.VendorID
	return &resp, nil
}

func mapErr(err error) error {
	switch {
	case errors.Is(err, identity.ErrRoleMismatch):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, identity.ErrAccountSuspended):
		return huma.NewError(http.StatusLocked, err.Error())
	case errors.Is(err, identity.ErrInvalidProvider),
		errors.Is(err, identity.ErrInvalidRole),
		errors.Is(err, identity.ErrInvalidClaims),
		errors.Is(err, oidc.ErrStateNotFound),
		errors.Is(err, oidc.ErrStateConsumed):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, identity.ErrUserNotFound),
		errors.Is(err, identity.ErrSessionNotFound):
		return huma.Error404NotFound(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
