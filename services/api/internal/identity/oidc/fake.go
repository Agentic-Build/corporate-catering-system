package oidc

import (
	"context"
	"fmt"
	"net/http"
)

// FakeProvider is a deterministic OIDC stand-in for local dev / e2e.
// It satisfies the Provider interface and bypasses real Google/GitHub.
// Enable by setting FAKE_OIDC=1 at process startup; never wire this
// into a real production deployment.
type FakeProvider struct {
	ProviderName string
	Userinfo     *Userinfo
	BaseURL      string // e.g. http://localhost:8080
}

func (f *FakeProvider) Name() string { return f.ProviderName }

func (f *FakeProvider) BuildAuthURL(_ context.Context, state string) (*AuthURL, error) {
	return &AuthURL{
		URL: fmt.Sprintf("%s/test/oidc/%s/authorize?state=%s", f.BaseURL, f.ProviderName, state),
	}, nil
}

func (f *FakeProvider) Exchange(_ context.Context, _ string, _ string, _ string) (*Userinfo, error) {
	return f.Userinfo, nil
}

// MountAutoredirect registers a handler on the given mux at
// /test/oidc/{provider}/authorize that immediately redirects to the provider
// callback with a fake authorization code.
func (f *FakeProvider) MountAutoredirect(mux *http.ServeMux, callback string) {
	pattern := fmt.Sprintf("/test/oidc/%s/authorize", f.ProviderName)
	mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		http.Redirect(w, r, fmt.Sprintf("%s?state=%s&code=fake", callback, state), http.StatusFound)
	})
}
