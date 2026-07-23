package cmd

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResolveStorageConfig(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantBackend storageBackend
		wantErr     bool
		check       func(t *testing.T, cfg storageConfig)
	}{
		{
			name:        "default is sqlite with default path",
			env:         map[string]string{},
			wantBackend: backendSQLite,
			check: func(t *testing.T, cfg storageConfig) {
				if cfg.sqlitePath != "chronos.db" {
					t.Errorf("sqlitePath = %q, want chronos.db", cfg.sqlitePath)
				}
			},
		},
		{
			name:        "explicit sqlite honors CHRONOS_DB_PATH",
			env:         map[string]string{envStorageBackend: "sqlite", envDBPath: "/tmp/custom.db"},
			wantBackend: backendSQLite,
			check: func(t *testing.T, cfg storageConfig) {
				if cfg.sqlitePath != "/tmp/custom.db" {
					t.Errorf("sqlitePath = %q, want /tmp/custom.db", cfg.sqlitePath)
				}
			},
		},
		{
			name:        "backend is case-insensitive and trimmed",
			env:         map[string]string{envStorageBackend: "  POSTGRES  ", envStorageDSN: "postgres://u@h/db"},
			wantBackend: backendPostgres,
			check: func(t *testing.T, cfg storageConfig) {
				if cfg.dsn != "postgres://u@h/db" {
					t.Errorf("dsn = %q", cfg.dsn)
				}
			},
		},
		{
			name:    "postgres missing DSN errors",
			env:     map[string]string{envStorageBackend: "postgres"},
			wantErr: true,
		},
		{
			name:        "redis parses URL into addr/password/db",
			env:         map[string]string{envStorageBackend: "redis", envRedisURL: "redis://:secret@localhost:6380/3"},
			wantBackend: backendRedis,
			check: func(t *testing.T, cfg storageConfig) {
				if cfg.redisAddr != "localhost:6380" {
					t.Errorf("redisAddr = %q, want localhost:6380", cfg.redisAddr)
				}
				if cfg.redisPassword != "secret" {
					t.Errorf("redisPassword = %q, want secret", cfg.redisPassword)
				}
				if cfg.redisDB != 3 {
					t.Errorf("redisDB = %d, want 3", cfg.redisDB)
				}
			},
		},
		{
			name:        "redis falls back to CHRONOS_STORAGE_DSN",
			env:         map[string]string{envStorageBackend: "redis", envStorageDSN: "redis://localhost:6379/0"},
			wantBackend: backendRedis,
			check: func(t *testing.T, cfg storageConfig) {
				if cfg.redisAddr != "localhost:6379" {
					t.Errorf("redisAddr = %q", cfg.redisAddr)
				}
			},
		},
		{
			name:    "redis missing URL errors",
			env:     map[string]string{envStorageBackend: "redis"},
			wantErr: true,
		},
		{
			name:    "redis invalid URL errors",
			env:     map[string]string{envStorageBackend: "redis", envRedisURL: "not-a-url"},
			wantErr: true,
		},
		{
			name:    "unknown backend errors",
			env:     map[string]string{envStorageBackend: "cassandra"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := resolveStorageConfig(tt.env)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (cfg=%+v)", cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.backend != tt.wantBackend {
				t.Fatalf("backend = %q, want %q", cfg.backend, tt.wantBackend)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestSharedStateDialect(t *testing.T) {
	tests := []struct {
		name        string
		backend     storageBackend
		env         map[string]string
		wantDialect string
		wantEnabled bool
	}{
		{
			name:        "postgres enabled by default",
			backend:     backendPostgres,
			env:         map[string]string{},
			wantDialect: "postgres",
			wantEnabled: true,
		},
		{
			name:        "postgres opt-out",
			backend:     backendPostgres,
			env:         map[string]string{envSharedState: "false"},
			wantEnabled: false,
		},
		{
			name:        "sqlite disabled by default",
			backend:     backendSQLite,
			env:         map[string]string{},
			wantEnabled: false,
		},
		{
			name:        "sqlite opt-in",
			backend:     backendSQLite,
			env:         map[string]string{envSharedState: "true"},
			wantDialect: "sqlite",
			wantEnabled: true,
		},
		{
			name:        "redis never shared even when opted in",
			backend:     backendRedis,
			env:         map[string]string{envSharedState: "true"},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect, enabled := sharedStateDialect(storageConfig{backend: tt.backend}, tt.env)
			if enabled != tt.wantEnabled {
				t.Fatalf("enabled = %v, want %v", enabled, tt.wantEnabled)
			}
			if enabled && dialect != tt.wantDialect {
				t.Errorf("dialect = %q, want %q", dialect, tt.wantDialect)
			}
		})
	}
}

func TestSQLDriverDSN(t *testing.T) {
	drv, dsn := sqlDriverDSN(storageConfig{backend: backendPostgres, dsn: "postgres://x"})
	if drv != "pgx" || dsn != "postgres://x" {
		t.Errorf("postgres: got (%q,%q)", drv, dsn)
	}

	drv, dsn = sqlDriverDSN(storageConfig{backend: backendSQLite, sqlitePath: "chronos.db"})
	if drv != "sqlite" {
		t.Errorf("sqlite driver = %q", drv)
	}
	if dsn != "chronos.db?_time_format=sqlite" {
		t.Errorf("sqlite dsn = %q", dsn)
	}
}

func TestOpenStoreFromConfigSQLite(t *testing.T) {
	dir := t.TempDir()
	cfg := storageConfig{backend: backendSQLite, sqlitePath: filepath.Join(dir, "smoke.db")}

	store, err := openStoreFromConfig(cfg)
	if err != nil {
		t.Fatalf("openStoreFromConfig: %v", err)
	}
	defer store.Close()

	// The store should be usable (migrations ran): a listing must not error.
	if _, err := store.ListSessions(context.Background(), "", 1, 0); err != nil {
		t.Fatalf("ListSessions on migrated store: %v", err)
	}
}

func TestBuildSharedStateOptions(t *testing.T) {
	// Redis: no SQL shared state, no options, nil closer.
	opts, closer, err := buildSharedStateOptions(storageConfig{backend: backendRedis}, map[string]string{})
	if err != nil {
		t.Fatalf("redis: unexpected error: %v", err)
	}
	if len(opts) != 0 || closer != nil {
		t.Fatalf("redis: expected no shared-state wiring, got opts=%d closer=%v", len(opts), closer != nil)
	}

	// SQLite opted in: scheduler + rate limiter wired against a temp DB file.
	dir := t.TempDir()
	cfg := storageConfig{backend: backendSQLite, sqlitePath: filepath.Join(dir, "shared.db")}
	opts, closer, err = buildSharedStateOptions(cfg, map[string]string{envSharedState: "true"})
	if err != nil {
		t.Fatalf("sqlite shared: %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("sqlite shared: expected 2 options, got %d", len(opts))
	}
	if closer == nil {
		t.Fatal("sqlite shared: expected a non-nil closer")
	}
	if err := closer(); err != nil {
		t.Fatalf("closer: %v", err)
	}
}
