package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"

	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/os/auth"
	"github.com/spawn08/chronos/os/middleware"
	"github.com/spawn08/chronos/os/scheduler"
)

// serveEnvKeys enumerates the environment variables that configure `chronos
// serve` authentication and CORS. They are read via the injected lookup map in
// buildServeOptions so the builder stays pure and unit-testable.
const (
	envAuthMode    = "CHRONOS_AUTH"         // none | jwt | apikey
	envJWTSecret   = "CHRONOS_JWT_SECRET"   // HS256 shared secret
	envJWTIssuer   = "CHRONOS_JWT_ISSUER"   // enforced iss claim
	envJWTAudience = "CHRONOS_JWT_AUDIENCE" // enforced aud claim
	envJWTJWKSURL  = "CHRONOS_JWT_JWKS_URL" // OIDC/JWKS endpoint for RS256
	envAPIKeys     = "CHRONOS_API_KEYS"     // comma list of "key:role:tenant"
	envCORSOrigins = "CHRONOS_CORS_ORIGINS" // comma list of allowed origins
	envSwagger     = "CHRONOS_SWAGGER"      // "false" disables the /swagger UI + spec
	envRBAC        = "CHRONOS_RBAC"         // "true" enforces method-based RBAC on /api/*
	envSharedState = "CHRONOS_SHARED_STATE" // opt in/out of cross-replica shared scheduler + rate limiter
)

// buildServeOptions translates environment configuration into ChronosOS server
// options. It is a pure function of the supplied env map so it can be unit
// tested without binding a port or touching the process environment.
//
// The returned mode string names the active auth mode ("none", "jwt", or
// "apikey") for startup logging. The default (empty or "none" CHRONOS_AUTH) is
// no authentication, preserving backward-compatible behavior.
func buildServeOptions(env map[string]string) ([]chronosos.Option, string, error) {
	var opts []chronosos.Option

	// CORS origins apply regardless of the auth mode.
	if origins := splitCSV(env[envCORSOrigins]); len(origins) > 0 {
		cfg := middleware.DefaultCORSConfig()
		cfg.AllowOrigins = origins
		opts = append(opts, chronosos.WithCORS(cfg))
	}

	// Swagger is enabled by default; only append an option when disabling it so
	// the server default (enabled) is preserved otherwise.
	if v, ok := parseBool(env[envSwagger]); ok && !v {
		opts = append(opts, chronosos.WithSwagger(false))
	}

	// RBAC is opt-in and only meaningful alongside authentication.
	if v, ok := parseBool(env[envRBAC]); ok && v {
		opts = append(opts, chronosos.WithRBAC(true))
	}

	mode := strings.ToLower(strings.TrimSpace(env[envAuthMode]))
	switch mode {
	case "", "none":
		return opts, "none", nil

	case "jwt":
		secret := strings.TrimSpace(env[envJWTSecret])
		jwksURL := strings.TrimSpace(env[envJWTJWKSURL])
		if secret == "" && jwksURL == "" {
			return nil, "", fmt.Errorf("build serve options: %s=jwt requires %s or %s to be set", envAuthMode, envJWTSecret, envJWTJWKSURL)
		}
		opts = append(opts, chronosos.WithJWTAuth(auth.JWTConfig{
			Secret:   secret,
			Issuer:   strings.TrimSpace(env[envJWTIssuer]),
			Audience: strings.TrimSpace(env[envJWTAudience]),
			JWKSURL:  jwksURL,
		}))
		return opts, "jwt", nil

	case "apikey":
		keys, err := parseAPIKeys(env[envAPIKeys])
		if err != nil {
			return nil, "", err
		}
		if len(keys) == 0 {
			return nil, "", fmt.Errorf("build serve options: %s=apikey requires %s with at least one entry", envAuthMode, envAPIKeys)
		}
		opts = append(opts, chronosos.WithAPIKeyAuth(auth.APIKeyConfig{Keys: keys}))
		return opts, "apikey", nil

	default:
		return nil, "", fmt.Errorf("build serve options: unknown %s=%q (want none, jwt, or apikey)", envAuthMode, mode)
	}
}

// parseBool interprets common truthy/falsy env spellings. The second return
// value reports whether the input was set to a recognized value; unset or
// unrecognized input returns (false, false) so callers keep their default.
func parseBool(raw string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
	}
}

// parseAPIKeys parses a comma-separated list of "key:role:tenant" entries into
// an APIKeyConfig key map. The role and tenant fields are optional; a bare key
// is granted the default "user" role. An empty key segment is an error.
//
// Because ':' is the field delimiter (and ',' the entry delimiter), key values
// must not contain ':' or ','; only the first two colons are treated as
// delimiters, and the tenant field (after the second colon) may itself contain
// colons.
func parseAPIKeys(raw string) (map[string]auth.APIKeyEntry, error) {
	entries := splitCSV(raw)
	if len(entries) == 0 {
		return nil, nil
	}
	keys := make(map[string]auth.APIKeyEntry, len(entries))
	for _, entry := range entries {
		parts := strings.SplitN(entry, ":", 3)
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, fmt.Errorf("parse api keys: empty key in entry %q", entry)
		}
		role := "user"
		if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			role = strings.TrimSpace(parts[1])
		}
		tenant := ""
		if len(parts) > 2 {
			tenant = strings.TrimSpace(parts[2])
		}
		keys[key] = auth.APIKeyEntry{Scope: role, TenantID: tenant}
	}
	return keys, nil
}

// serveSharedEnv snapshots the shared-state environment into a map so the pure
// sharedStateDialect decision can be unit tested in isolation.
func serveSharedEnv() map[string]string {
	return map[string]string{envSharedState: os.Getenv(envSharedState)}
}

// sharedStateDialect decides whether cross-replica shared scheduler and
// rate-limiter state should be wired for the given storage backend, and which
// SQL dialect to use. It is pure so the gating rule is unit-testable.
//
// Rule:
//   - postgres: enabled by default (a shared, durable SQL store); an explicit
//     CHRONOS_SHARED_STATE=false disables it.
//   - sqlite: disabled by default (single-node dev); enabled only with an
//     explicit CHRONOS_SHARED_STATE=true.
//   - redis: never — the shared scheduler/limiter require a SQL (*sql.DB)
//     backend, which Redis does not provide.
func sharedStateDialect(cfg storageConfig, env map[string]string) (dialect string, enabled bool) {
	override, overrideSet := parseBool(env[envSharedState])
	switch cfg.backend {
	case backendPostgres:
		if overrideSet && !override {
			return "", false
		}
		return "postgres", true
	case backendSQLite:
		if overrideSet && override {
			return "sqlite", true
		}
		return "", false
	default:
		return "", false
	}
}

// sqlDriverDSN returns the database/sql driver name and DSN for opening a second
// connection pool to the same SQL database as the storage adapter. The scheduler
// and rate limiter own their own tables, so a separate pool to the same database
// is genuinely shared cross-replica state, not a fake. The sqlite DSN mirrors the
// adapter's _time_format so timestamp columns round-trip and compare correctly.
func sqlDriverDSN(cfg storageConfig) (driver, dsn string) {
	switch cfg.backend {
	case backendPostgres:
		return "pgx", cfg.dsn
	case backendSQLite:
		path := cfg.sqlitePath
		if !strings.Contains(path, "_time_format=") {
			sep := "?"
			if strings.Contains(path, "?") {
				sep = "&"
			}
			path += sep + "_time_format=sqlite"
		}
		return "sqlite", path
	default:
		return "", ""
	}
}

// buildSharedStateOptions wires a store-backed scheduler (exactly-once cron
// firing across replicas) and a shared SQL rate limiter (cluster-wide limits)
// when the backend qualifies per sharedStateDialect. It opens a dedicated
// *sql.DB to the same database (the adapters do not expose their pool, and the
// scheduler/limiter own separate tables, so this is correct — not a fake share)
// and returns a closer for that pool. When shared state does not apply it
// returns no options and a nil closer, leaving the server's in-process defaults
// intact.
func buildSharedStateOptions(cfg storageConfig, env map[string]string) ([]chronosos.Option, func() error, error) {
	dialect, enabled := sharedStateDialect(cfg, env)
	if !enabled {
		if cfg.backend == backendRedis {
			log.Printf("Rate limiter: in-process per-replica (Redis is not SQL-backed; no shared SQL limiter/scheduler)")
		}
		return nil, nil, nil
	}

	driver, dsn := sqlDriverDSN(cfg)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open shared-state db: %w", err)
	}

	ctx := context.Background()

	// The scheduler store and rate limiter share this *sql.DB. We manage the
	// pool lifecycle here (via the returned closer) rather than through the
	// scheduler store's Close, which would close the DB out from under the
	// limiter.
	schedStore := scheduler.NewSQLStore(db, scheduler.Dialect(dialect))
	sched := scheduler.NewStoreScheduler(schedStore, func(_ context.Context, _, _, _ string) error {
		// Matches the server's default in-process scheduler: no agent runner is
		// wired from the CLI, so a fired schedule reports this clearly.
		return fmt.Errorf("no agent runner configured")
	})
	if err := sched.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("migrate scheduler store: %w", err)
	}

	limiter := middleware.NewSQLLimiter(db, middleware.Dialect(dialect))
	if err := limiter.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("migrate rate limiter store: %w", err)
	}

	log.Printf("Cross-replica shared state enabled (%s): store-backed scheduler + shared SQL rate limiter", dialect)

	opts := []chronosos.Option{
		chronosos.WithScheduler(sched),
		chronosos.WithRateLimiter(limiter),
	}
	return opts, db.Close, nil
}

// splitCSV splits a comma-separated string into trimmed, non-empty fields.
func splitCSV(s string) []string {
	var out []string
	for _, field := range strings.Split(s, ",") {
		if f := strings.TrimSpace(field); f != "" {
			out = append(out, f)
		}
	}
	return out
}
