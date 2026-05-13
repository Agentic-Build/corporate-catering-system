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

func (a *API) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h == "" || !strings.HasPrefix(h, "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		sess, err := a.Sessions.Get(r.Context(), tok)
		if err != nil {
			if errors.Is(err, identity.ErrSessionNotFound) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "auth error", http.StatusInternalServerError)
			return
		}
		u, err := a.Users.GetByID(r.Context(), sess.UserID)
		if err != nil {
			http.Error(w, "user lookup", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		ctx = context.WithValue(ctx, tokenCtxKey, tok)
		_ = a.Sessions.Touch(ctx, tok)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
