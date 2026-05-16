package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Slug         string
	DisplayName  string
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

type OIDCProvider struct {
	slug        string
	displayName string
	verifier    *gooidc.IDTokenVerifier
	oauth       *oauth2.Config
}

func New(ctx context.Context, cfg Config) (*OIDCProvider, error) {
	if cfg.Slug == "" {
		return nil, errors.New("oidc: slug is required")
	}
	if cfg.IssuerURL == "" {
		return nil, fmt.Errorf("oidc %s: issuer URL is required", cfg.Slug)
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("oidc %s: client ID is required", cfg.Slug)
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("oidc %s: client secret is required", cfg.Slug)
	}
	if cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc %s: redirect URL is required", cfg.Slug)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "email", "profile"}
	}
	if !hasScope(scopes, gooidc.ScopeOpenID) {
		scopes = append([]string{gooidc.ScopeOpenID}, scopes...)
	}
	provider, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc %s discovery: %w", cfg.Slug, err)
	}
	displayName := cfg.DisplayName
	if displayName == "" {
		displayName = cfg.Slug
	}
	return &OIDCProvider{
		slug:        cfg.Slug,
		displayName: displayName,
		verifier:    provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}, nil
}

func (p *OIDCProvider) Name() string { return p.slug }

func (p *OIDCProvider) DisplayName() string { return p.displayName }

func (p *OIDCProvider) BuildAuthURL(_ context.Context, state string) (*AuthURL, error) {
	verifier := randURLSafe(48)
	nonce := randURLSafe(24)
	challenge := s256(verifier)
	u := p.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	return &AuthURL{URL: u, PKCEVerifier: verifier, Nonce: nonce}, nil
}

func (p *OIDCProvider) Exchange(ctx context.Context, code, verifier, expectNonce string) (*Userinfo, error) {
	tok, err := p.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, fmt.Errorf("oidc %s token exchange: %w", p.slug, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("oidc %s: no id_token", p.slug)
	}
	idt, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("oidc %s verify id_token: %w", p.slug, err)
	}
	if idt.Nonce != expectNonce {
		return nil, fmt.Errorf("oidc %s: nonce mismatch", p.slug)
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		PreferredName string `json:"preferred_username"`
	}
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc %s decode claims: %w", p.slug, err)
	}
	rawClaims := map[string]any{}
	_ = idt.Claims(&rawClaims)
	name := claims.Name
	if name == "" {
		name = claims.PreferredName
	}
	if name == "" {
		name = claims.Email
	}
	return &Userinfo{
		Provider:        p.slug,
		ExternalSubject: claims.Sub,
		Email:           strings.ToLower(claims.Email),
		EmailVerified:   claims.EmailVerified,
		DisplayName:     name,
		Raw:             rawClaims,
	}, nil
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}
