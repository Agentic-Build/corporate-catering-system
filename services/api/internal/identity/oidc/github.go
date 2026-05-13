package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type GitHubProvider struct {
	oauth *oauth2.Config
}

func NewGitHub(clientID, clientSecret, redirectURL string) *GitHubProvider {
	return &GitHubProvider{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     github.Endpoint,
			Scopes:       []string{"read:user", "user:email"},
		},
	}
}

func (g *GitHubProvider) Name() string { return "github" }

func (g *GitHubProvider) BuildAuthURL(_ context.Context, state string) (*AuthURL, error) {
	return &AuthURL{URL: g.oauth.AuthCodeURL(state)}, nil
}

func (g *GitHubProvider) Exchange(ctx context.Context, code, _verifier, _nonce string) (*Userinfo, error) {
	tok, err := g.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	client := g.oauth.Client(ctx, tok)

	var user struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("github /user: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("github /user status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode /user: %w", err)
	}

	if user.Email == "" {
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		r, err := client.Get("https://api.github.com/user/emails")
		if err != nil {
			return nil, fmt.Errorf("github /user/emails: %w", err)
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&emails); err != nil {
			return nil, fmt.Errorf("decode emails: %w", err)
		}
		for _, e := range emails {
			if e.Primary && e.Verified {
				user.Email = e.Email
				break
			}
		}
	}
	if user.Email == "" {
		return nil, errors.New("github: no verified primary email")
	}

	name := user.Name
	if name == "" {
		name = user.Login
	}
	return &Userinfo{
		Provider:        "github",
		ExternalSubject: fmt.Sprintf("%d", user.ID),
		Email:           strings.ToLower(user.Email),
		EmailVerified:   true,
		DisplayName:     name,
		Raw: map[string]any{
			"id":    user.ID,
			"login": user.Login,
			"name":  user.Name,
			"email": user.Email,
		},
	}, nil
}
