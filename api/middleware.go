// Package api provides a JSON REST API for reading and writing wiki pages.
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// authMiddleware enforces Bearer token authentication.
// If BOOK_API_TOKEN is not set, all requests return 503.
// Missing Authorization header → 401. Wrong token → 403.
func authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := os.Getenv("BOOK_API_TOKEN")
		if token == "" {
			writeError(w, http.StatusServiceUnavailable, "API token not configured")
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeError(w, http.StatusUnauthorized, "authorization required")
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "invalid authorization format; expected 'Bearer <token>'")
			return
		}

		provided := strings.TrimPrefix(authHeader, "Bearer ")
		if provided != token {
			writeError(w, http.StatusForbidden, "invalid token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// writeJSON writes a JSON success response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
