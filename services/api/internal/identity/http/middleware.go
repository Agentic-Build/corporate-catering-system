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
// access-token verifier (kept local to avoid an import cycle on hydra).
type JWTVerifier interface {
	Verify(ctx context.Context, raw string) (subject string, err error)
}

func UserFromContext(ctx context.Context) (*identity.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*identity.User)
	return u, ok
}

// ContextWithUser attaches an authenticated user under the same key
// AuthMiddleware uses, so non-HTTP entrypoints (MCP stdio, tests) populate
// the request context the same way.
func ContextWithUser(ctx context.Context, u *identity.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

func TokenFromContext(ctx context.Context) (string, bool) {
	t, ok := ctx.Value(tokenCtxKey).(string)
	return t, ok
}

// AuthMiddleware authenticates the caller by Bearer token. Tries in order:
// (1) T-Bite session token (`tb_…`) in Redis, (2) Hydra-issued JWT access
// token validated against JWKS. JWT path is skipped when API.JWT is nil.
// Invalid tokens fall through to anonymous handling.
func (a *API) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			next.ServeHTTP(w, r)
			return
		}

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

		// Hydra JWT path — only when wired (api role).
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
