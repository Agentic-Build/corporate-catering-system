package idhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/takalawang/corporate-catering-system/services/api/internal/identity"
)

type ctxKey int

const (
	userCtxKey ctxKey = iota
	tokenCtxKey
)

// JWTVerifier is the small surface AuthMiddleware needs from the Hydra
// access-token verifier so we don't import a hydra package here and create
// a cycle. The hydra package wires the concrete implementation; tests
// substitute a stub.
type JWTVerifier interface {
	Verify(ctx context.Context, raw string) (subject string, err error)
}

func UserFromContext(ctx context.Context) (*identity.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*identity.User)
	return u, ok
}

// ContextWithUser attaches an authenticated user to ctx under the same
// unexported key AuthMiddleware uses, so non-HTTP entrypoints (MCP stdio,
// tests, etc.) can populate the request context the same way the middleware
// does on the HTTP path.
func ContextWithUser(ctx context.Context, u *identity.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

func TokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenCtxKey).(string)
	return t, ok
}

// AuthMiddleware authenticates the caller by Bearer token. It tries two
// token shapes in order:
//
//  1. T-Bite session token (the `tb_…` value the SvelteKit frontends use,
//     and the historical MCP credential). Looked up in Redis.
//  2. Hydra-issued JWT access token (`eyJ…`), validated against Hydra's
//     JWKS. Subject claim is the T-Bite user ID.
//
// JWT validation is skipped when API.JWT is nil — the api role wires it,
// the mcp-stdio role doesn't.
//
// On either successful lookup the user is loaded and attached to ctx;
// failures fall through to anonymous handling (matching the historical
// behaviour for invalid session tokens).
func (a *API) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 1. Session-token path (legacy + web).
		sess, err := a.Sessions.Get(r.Context(), tok)
		switch {
		case err == nil:
			u, lookupErr := a.Users.GetByID(r.Context(), sess.UserID)
			if lookupErr != nil {
				http.Error(w, "user lookup", http.StatusInternalServerError)
				return
			}
			if u.Status != identity.StatusActive {
				http.Error(w, "account suspended", http.StatusLocked)
				return
			}
			ctx := context.WithValue(r.Context(), userCtxKey, u)
			ctx = context.WithValue(ctx, tokenCtxKey, tok)
			_ = a.Sessions.Touch(ctx, tok)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		case errors.Is(err, identity.ErrSessionNotFound):
			// fallthrough to JWT path
		default:
			http.Error(w, "auth error", http.StatusInternalServerError)
			return
		}

		// 2. Hydra JWT path. Only attempted when the api role wired a
		// verifier; mcp-stdio and tests skip this branch.
		if a.JWT == nil {
			next.ServeHTTP(w, r)
			return
		}
		subject, err := a.JWT.Verify(r.Context(), tok)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		u, err := a.Users.GetByID(r.Context(), subject)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if u.Status != identity.StatusActive {
			http.Error(w, "account suspended", http.StatusLocked)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		ctx = context.WithValue(ctx, tokenCtxKey, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
