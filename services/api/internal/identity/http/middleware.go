package idhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Agentic-Build/corporate-catering-system/services/api/internal/identity"
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

// trySessionAuth resolves the bearer token against the session store, returning
// (handled, halted). When handled=true the request was either authenticated
// (next.ServeHTTP fired) or an error was written, so the middleware must stop.
func (a *API) trySessionAuth(w http.ResponseWriter, r *http.Request, next http.Handler, tok string) (handled bool) {
	sess, err := a.Sessions.Get(r.Context(), tok)
	switch {
	case err == nil:
		u, lookupErr := a.Users.GetByID(r.Context(), sess.UserID)
		if lookupErr != nil {
			http.Error(w, "user lookup", http.StatusInternalServerError)
			return true
		}
		if u.Status != identity.StatusActive {
			http.Error(w, "account suspended", http.StatusLocked)
			return true
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		ctx = context.WithValue(ctx, tokenCtxKey, tok)
		_ = a.Sessions.Touch(ctx, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
		return true
	case errors.Is(err, identity.ErrSessionNotFound):
		return false
	default:
		http.Error(w, "auth error", http.StatusInternalServerError)
		return true
	}
}

// tryJWTAuth resolves the bearer token via Hydra JWKS. Invalid tokens fall
// through (next.ServeHTTP) so the request continues unauthenticated.
func (a *API) tryJWTAuth(w http.ResponseWriter, r *http.Request, next http.Handler, tok string) {
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
		if a.trySessionAuth(w, r, next, tok) {
			return
		}
		a.tryJWTAuth(w, r, next, tok)
	})
}
