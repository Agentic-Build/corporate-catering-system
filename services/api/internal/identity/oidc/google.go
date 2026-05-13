package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type GoogleProvider struct {
	verif *oidc.IDTokenVerifier
	oauth *oauth2.Config
}

func NewGoogle(ctx context.Context, clientID, clientSecret, redirectURL string) (*GoogleProvider, error) {
	p, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("google oidc discovery: %w", err)
	}
	return &GoogleProvider{
		verif: p.Verifier(&oidc.Config{ClientID: clientID}),
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
	}, nil
}

func (g *GoogleProvider) Name() string { return "google" }

func (g *GoogleProvider) BuildAuthURL(_ context.Context, state string) (*AuthURL, error) {
	verifier := randURLSafe(48)
	nonce := randURLSafe(24)
	challenge := s256(verifier)
	url := g.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	return &AuthURL{URL: url, PKCEVerifier: verifier, Nonce: nonce}, nil
}

func (g *GoogleProvider) Exchange(ctx context.Context, code, verifier, expectNonce string) (*Userinfo, error) {
	tok, err := g.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errors.New("google: no id_token")
	}
	idt, err := g.verif.Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}
	if idt.Nonce != expectNonce {
		return nil, errors.New("google: nonce mismatch")
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode claims: %w", err)
	}
	var rawClaims map[string]any
	_ = idt.Claims(&rawClaims)
	return &Userinfo{
		Provider:        "google",
		ExternalSubject: claims.Sub,
		Email:           strings.ToLower(claims.Email),
		EmailVerified:   claims.EmailVerified,
		DisplayName:     claims.Name,
		Raw:             rawClaims,
	}, nil
}
