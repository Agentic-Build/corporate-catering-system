package idhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

// AppBaseURLs maps app names ("employee"|"merchant"|"admin") to their public base URLs.
type AppBaseURLs map[string]string

type API struct {
	Svc      *identity.Service
	Sessions identity.SessionStore
	Users    identity.UserRepository
	AppURLs  AppBaseURLs

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
		return nil, mapErr(err)
	}

	base, ok := a.AppURLs[out.App]
	if !ok {
		return nil, huma.Error500InternalServerError("unknown app base url for " + out.App)
	}
	landing := fmt.Sprintf("%s/auth/landing?token=%s&return_to=%s",
		base,
		url.QueryEscape(out.Session.Token),
		url.QueryEscape(out.ReturnTo),
	)
	return &completeLoginOutput{Status: http.StatusFound, Url: landing}, nil
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
		errors.Is(err, identity.ErrInvalidClaims):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, identity.ErrUserNotFound),
		errors.Is(err, identity.ErrSessionNotFound):
		return huma.Error404NotFound(err.Error())
	}
	return huma.Error500InternalServerError("internal", err)
}
