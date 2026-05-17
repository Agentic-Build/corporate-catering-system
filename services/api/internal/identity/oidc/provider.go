package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

type Userinfo struct {
	Provider        string
	ExternalSubject string
	Email           string
	EmailVerified   bool
	DisplayName     string
	Raw             map[string]any
}

type ProviderInfo struct {
	Slug        string
	DisplayName string
}

type AuthURL struct {
	URL          string
	PKCEVerifier string
	Nonce        string
}

type Provider interface {
	Name() string
	DisplayName() string
	BuildAuthURL(ctx context.Context, state string) (*AuthURL, error)
	Exchange(ctx context.Context, code, pkceVerifier, nonce string) (*Userinfo, error)
}

func randURLSafe(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}
