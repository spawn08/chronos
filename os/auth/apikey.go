package auth

import (
	"context"
	"fmt"
	"net/http"
)

// APIKeyConfig holds configuration for API key authentication.
//
// Backward compatibility: the legacy Keys map (raw key -> entry) is still
// accepted. At middleware construction the raw keys are hashed into an
// in-memory KeyStore and the plaintext is not retained. For a persisted,
// pre-hashed store, set Store instead (it takes precedence over Keys).
type APIKeyConfig struct {
	// HeaderName is the request header carrying the key (default: X-Api-Key).
	HeaderName string
	// Keys is the legacy inline key map. Raw keys are hashed at construction.
	Keys map[string]APIKeyEntry
	// Store is an optional persisted, hashed key store. When set it is used
	// instead of Keys.
	Store KeyStore
	// SkipPaths bypass authentication entirely.
	SkipPaths []string

	// Quotas, when set, enforces per-key and per-tenant quotas on each request.
	Quotas QuotaStore
	// TenantQuotas maps a tenant ID to its quota, enforced when Quotas is set.
	TenantQuotas map[string]Quota
}

// APIKeyEntry represents a configured API key with associated metadata.
type APIKeyEntry struct {
	Scope    string
	UserID   string
	TenantID string
	Quota    Quota
}

// APIKeyMiddleware returns HTTP middleware that validates API keys
// from the configured header (default: X-Api-Key).
//
// Keys are matched against a hashed KeyStore using constant-time comparison;
// raw key material is never retained in the middleware. When Quotas is set,
// per-key and per-tenant limits are enforced (HTTP 429 on breach).
func APIKeyMiddleware(cfg APIKeyConfig) func(http.Handler) http.Handler {
	headerName := cfg.HeaderName
	if headerName == "" {
		headerName = "X-Api-Key"
	}

	skipSet := make(map[string]bool, len(cfg.SkipPaths))
	for _, p := range cfg.SkipPaths {
		skipSet[p] = true
	}

	store := cfg.Store
	if store == nil {
		store = buildMemoryStore(cfg.Keys)
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

			rec, ok, err := store.Lookup(r.Context(), key)
			if err != nil {
				http.Error(w, `{"error":"key store error"}`, http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
				return
			}

			if cfg.Quotas != nil {
				if allowed, qerr := enforceQuotas(r, cfg, rec); qerr != nil {
					http.Error(w, `{"error":"quota store error"}`, http.StatusInternalServerError)
					return
				} else if !allowed {
					http.Error(w, `{"error":"quota exceeded"}`, http.StatusTooManyRequests)
					return
				}
			}

			claims := &UserClaims{
				UserID:   rec.UserID,
				Roles:    []string{rec.Scope},
				TenantID: rec.TenantID,
			}
			ctx := WithUser(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// enforceQuotas checks per-key and per-tenant quotas for the request.
func enforceQuotas(r *http.Request, cfg APIKeyConfig, rec *KeyRecord) (bool, error) {
	if !rec.Quota.IsZero() {
		allowed, err := cfg.Quotas.Allow(r.Context(), "key:"+rec.ID, rec.Quota)
		if err != nil || !allowed {
			return allowed, err
		}
	}
	if rec.TenantID != "" {
		if tq, ok := cfg.TenantQuotas[rec.TenantID]; ok && !tq.IsZero() {
			allowed, err := cfg.Quotas.Allow(r.Context(), "tenant:"+rec.TenantID, tq)
			if err != nil || !allowed {
				return allowed, err
			}
		}
	}
	return true, nil
}

// buildMemoryStore hashes the legacy inline key map into an in-memory store.
func buildMemoryStore(keys map[string]APIKeyEntry) KeyStore {
	store := NewMemoryKeyStore()
	for raw, entry := range keys {
		// IDs are derived from the key hash inside Add when left empty.
		_, _ = store.Add(context.Background(), raw, KeyRecord{
			Scope:    entry.Scope,
			UserID:   entry.UserID,
			TenantID: entry.TenantID,
			Quota:    entry.Quota,
		})
	}
	return store
}
