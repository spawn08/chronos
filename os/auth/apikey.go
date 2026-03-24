package auth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
)

// APIKeyConfig holds configuration for API key authentication.
type APIKeyConfig struct {
	HeaderName string
	Keys       map[string]APIKeyEntry
	SkipPaths  []string
}

// APIKeyEntry represents a configured API key with associated metadata.
type APIKeyEntry struct {
	Scope  string
	UserID string
}

// APIKeyMiddleware returns HTTP middleware that validates API keys
// from the configured header (default: X-Api-Key).
func APIKeyMiddleware(cfg APIKeyConfig) func(http.Handler) http.Handler {
	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "X-Api-Key"
	}

	skipSet := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipSet[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get(headerName)
			if key == "" {
				http.Error(w, fmt.Sprintf(`{"error":"missing %s header"}`, headerName), http.StatusUnauthorized)
				return
			}

			var matched *APIKeyEntry
			for k, entry := range cfg.Keys {
				if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
					e := entry
					matched = &e
					break
				}
			}

			if matched == nil {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
				return
			}

			claims := &UserClaims{
				UserID: matched.UserID,
				Roles:  []string{matched.Scope},
			}
			ctx := r.Context()
			ctx = WithUser(ctx, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
