package auth

import (
	"net/http"
	"strings"
)

// Middleware returns an HTTP middleware that validates Bearer token auth.
// If token is empty, authentication is skipped (development mode).
// Invalid or missing tokens get a 401 with JSON error body.
func Middleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token == "" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" || parts[1] != token {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
