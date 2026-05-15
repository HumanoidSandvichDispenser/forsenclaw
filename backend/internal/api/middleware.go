package api

import (
	"context"
	"net/http"
)

// User is the authenticated user attached to the request context.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type contextKey int

const userContextKey contextKey = iota

// AuthMiddleware is a placeholder authentication middleware.
// In F11 this will validate session tokens. For now, all requests
// are treated as the root user.
func AuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := User{
				ID:   "local",
				Name: "User",
				Role: "owner",
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) (User, bool) {
	user, ok := ctx.Value(userContextKey).(User)
	return user, ok
}
