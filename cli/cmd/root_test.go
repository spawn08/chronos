package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// newTestStore creates an in-memory SQLite store for testing.
func newTestStore(t *testing.T) *sqlite.Store {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("New(:memory:): %v", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// captureStdout captures stdout output from fn.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestHumanizeBytes(t *testing.T) {
	tests := []struct {
		name string
		b    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"max bytes", 1023, "1023 B"},
		{"one KB", 1024, "1.0 KB"},
		{"KB range", 1536, "1.5 KB"},
		{"one MB", 1024 * 1024, "1.0 MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0 GB"},
		{"one TB", 1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := humanizeBytes(tt.b)
			if got != tt.want {
				t.Errorf("humanizeBytes(%d) = %q, want %q", tt.b, got, tt.want)
			}
		})
	}
}

func TestEnvOrDefault(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		setVal string
		def    string
		want   string
	}{
		{"env not set", "TEST_CMD_UNSET_XYZ", "", "fallback", "fallback"},
		{"env set", "TEST_CMD_SET_XYZ", "myval", "fallback", "myval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setVal != "" {
				os.Setenv(tt.key, tt.setVal)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}
			got := envOrDefault(tt.key, tt.def)
			if got != tt.want {
				t.Errorf("envOrDefault(%q, %q) = %q, want %q", tt.key, tt.def, got, tt.want)
			}
		})
	}
}

func TestMaskEnv(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		setVal string
		want   string
	}{
		{"not set", "TEST_MASK_UNSET_XYZ", "", "(not set)"},
		{"short key", "TEST_MASK_SHORT_XYZ", "abcd", "****"},
		{"exactly 8", "TEST_MASK_8_XYZ", "12345678", "****"},
		{"long key", "TEST_MASK_LONG_XYZ", "sk-1234567890abcdef", "sk-1...cdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setVal != "" {
				os.Setenv(tt.key, tt.setVal)
				defer os.Unsetenv(tt.key)
			} else {
				os.Unsetenv(tt.key)
			}
			got := maskEnv(tt.key)
			if got != tt.want {
				t.Errorf("maskEnv(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestStorageLabel(t *testing.T) {
	tests := []struct {
		name string
		cfg  agent.StorageConfig
		want string
	}{
		{"empty backend", agent.StorageConfig{}, "sqlite (default)"},
		{"backend only", agent.StorageConfig{Backend: "postgres"}, "postgres"},
		{"backend with dsn", agent.StorageConfig{Backend: "postgres", DSN: "host=localhost"}, "postgres (host=localhost)"},
		{
			"long dsn truncated",
			agent.StorageConfig{Backend: "postgres", DSN: "host=localhost port=5432 dbname=chronos_prod user=admin password=secret sslmode=require"},
			"postgres (host=localhost port=5432 dbname=chron...)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := storageLabel(tt.cfg)
			if got != tt.want {
				t.Errorf("storageLabel(%+v) = %q, want %q", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestPrintUsage(t *testing.T) {
	output := captureStdout(t, func() {
		printUsage()
	})
	for _, keyword := range []string{"chronos", "repl", "serve", "run", "agent", "team", "sessions", "memory", "config", "version", "help"} {
		if !strings.Contains(output, keyword) {
			t.Errorf("printUsage() output missing keyword %q", keyword)
		}
	}
}

func TestExecuteVersion(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "version"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute() error: %v", err)
		}
	})
	for _, keyword := range []string{"chronos ", "commit:", "built:", "go:", "os/arch:"} {
		if !strings.Contains(output, keyword) {
			t.Errorf("version output missing %q: %q", keyword, output)
		}
	}
}

func TestExecuteHelp(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			os.Args = []string{"chronos", arg}
			output := captureStdout(t, func() {
				if err := Execute(); err != nil {
					t.Fatalf("Execute() error: %v", err)
				}
			})
			if !strings.Contains(output, "chronos") {
				t.Errorf("help output missing 'chronos': %q", output)
			}
		})
	}
}

func TestExecuteNoArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute() error: %v", err)
		}
	})
	if !strings.Contains(output, "Usage") {
		t.Errorf("no-args output missing 'Usage': %q", output)
	}
}

func TestExecuteUnknownCommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "nonexistent"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' in error, got: %v", err)
	}
}

func TestSessionsList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		output := captureStdout(t, func() {
			if err := sessionsList(ctx, store, ""); err != nil {
				t.Fatalf("sessionsList: %v", err)
			}
		})
		if !strings.Contains(output, "No sessions found") {
			t.Errorf("expected 'No sessions found', got: %q", output)
		}
	})

	t.Run("with sessions", func(t *testing.T) {
		now := time.Now()
		store.CreateSession(ctx, &storage.Session{
			ID: "s1", AgentID: "agent-1", Status: "running",
			CreatedAt: now, UpdatedAt: now,
		})
		store.CreateSession(ctx, &storage.Session{
			ID: "s2", AgentID: "agent-1", Status: "completed",
			CreatedAt: now, UpdatedAt: now,
		})

		output := captureStdout(t, func() {
			if err := sessionsList(ctx, store, "agent-1"); err != nil {
				t.Fatalf("sessionsList: %v", err)
			}
		})
		if !strings.Contains(output, "s1") || !strings.Contains(output, "s2") {
			t.Errorf("sessionsList output missing session IDs: %q", output)
		}
		if !strings.Contains(output, "agent-1") {
			t.Errorf("sessionsList output missing agent ID: %q", output)
		}
	})
}

func TestSessionsExport(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID: "export-s1", AgentID: "agent-1", Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	})
	store.AppendEvent(ctx, &storage.Event{
		ID: "e1", SessionID: "export-s1", Type: "node_enter",
		SeqNum: 1, Payload: map[string]any{"node": "start"},
	})

	output := captureStdout(t, func() {
		if err := sessionsExport(ctx, store, "export-s1"); err != nil {
			t.Fatalf("sessionsExport: %v", err)
		}
	})
	for _, want := range []string{"export-s1", "agent-1", "completed", "Events (1)", "node_enter"} {
		if !strings.Contains(output, want) {
			t.Errorf("sessionsExport output missing %q: %q", want, output)
		}
	}
}

func TestMemoryList(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		output := captureStdout(t, func() {
			if err := memoryList(ctx, store, "agent-1"); err != nil {
				t.Fatalf("memoryList: %v", err)
			}
		})
		if !strings.Contains(output, "No memories found") {
			t.Errorf("expected 'No memories found', got: %q", output)
		}
	})

	t.Run("with memories", func(t *testing.T) {
		store.PutMemory(ctx, &storage.MemoryRecord{
			ID: "m1", AgentID: "agent-1", Kind: "long_term",
			Key: "user_name", Value: "Alice",
		})

		output := captureStdout(t, func() {
			if err := memoryList(ctx, store, "agent-1"); err != nil {
				t.Fatalf("memoryList: %v", err)
			}
		})
		if !strings.Contains(output, "user_name") || !strings.Contains(output, "Alice") {
			t.Errorf("memoryList output missing memory data: %q", output)
		}
	})
}

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		input   string
		want    team.Strategy
		wantErr bool
	}{
		{"sequential", team.StrategySequential, false},
		{"SEQUENTIAL", team.StrategySequential, false},
		{"parallel", team.StrategyParallel, false},
		{"router", team.StrategyRouter, false},
		{"coordinator", team.StrategyCoordinator, false},
		{"unknown", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseStrategy(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseStrategy(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseErrorStrategy(t *testing.T) {
	tests := []struct {
		input   string
		want    team.ErrorStrategy
		wantErr bool
	}{
		{"fail_fast", team.ErrorStrategyFailFast, false},
		{"failfast", team.ErrorStrategyFailFast, false},
		{"collect", team.ErrorStrategyCollect, false},
		{"best_effort", team.ErrorStrategyBestEffort, false},
		{"besteffort", team.ErrorStrategyBestEffort, false},
		{"unknown", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseErrorStrategy(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("parseErrorStrategy(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// --- P0-009: Config set/model tests ---

func TestConfigSet(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	output := captureStdout(t, func() {
		if err := configSet("model", "gpt-4-turbo"); err != nil {
			t.Fatalf("configSet: %v", err)
		}
	})
	if !strings.Contains(output, "Set model = gpt-4-turbo") {
		t.Errorf("expected confirmation output, got: %q", output)
	}

	// Verify file was created
	configPath := tmpDir + "/.chronos/config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "model: gpt-4-turbo") {
		t.Errorf("config file missing model entry: %q", content)
	}
}

func TestConfigSet_OverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Set initial value
	captureStdout(t, func() { configSet("model", "gpt-4") })
	captureStdout(t, func() { configSet("api_key", "sk-123") })

	// Overwrite model
	captureStdout(t, func() { configSet("model", "gpt-4o") })

	configPath := tmpDir + "/.chronos/config.yaml"
	data, _ := os.ReadFile(configPath)
	content := string(data)

	if !strings.Contains(content, "model: gpt-4o") {
		t.Errorf("model should be updated to gpt-4o: %q", content)
	}
	if !strings.Contains(content, "api_key: sk-123") {
		t.Errorf("api_key should be preserved: %q", content)
	}
}

func TestConfigSet_PreservesComments(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	// Create initial config with a comment
	configDir := tmpDir + "/.chronos"
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(configDir+"/config.yaml", []byte("# Chronos CLI configuration\nmodel: gpt-4\n"), 0o644)

	// Update via configSet
	captureStdout(t, func() { configSet("model", "claude") })

	data, _ := os.ReadFile(configDir + "/config.yaml")
	content := string(data)
	if !strings.Contains(content, "model: claude") {
		t.Errorf("model should be updated: %q", content)
	}
	if !strings.Contains(content, "# Chronos CLI configuration") {
		t.Errorf("header comment should be present: %q", content)
	}
}

func TestConfigLookup(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	configDir := tmpDir + "/.chronos"
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(configDir+"/config.yaml", []byte("model: gpt-4-turbo\ndb_path: /tmp/test.db\n"), 0o644)

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"direct match", "model", "gpt-4-turbo"},
		{"env-style prefix", "CHRONOS_MODEL", "gpt-4-turbo"},
		{"different key", "db_path", "/tmp/test.db"},
		{"env-style different", "CHRONOS_DB_PATH", "/tmp/test.db"},
		{"non-existent", "nonexistent", ""},
		{"CHRONOS_NONEXISTENT", "CHRONOS_NONEXISTENT", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configLookup(tt.key)
			if got != tt.want {
				t.Errorf("configLookup(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestConfigLookup_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	got := configLookup("model")
	if got != "" {
		t.Errorf("configLookup should return empty when no config file exists, got %q", got)
	}
}

func TestEnvOrDefault_FallsBackToConfig(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	configDir := tmpDir + "/.chronos"
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(configDir+"/config.yaml", []byte("model: from-config\n"), 0o644)

	os.Unsetenv("CHRONOS_MODEL")
	got := envOrDefault("CHRONOS_MODEL", "default-val")
	if got != "from-config" {
		t.Errorf("envOrDefault should fall back to config file, got %q", got)
	}
}

func TestEnvOrDefault_EnvTakesPrecedenceOverConfig(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	configDir := tmpDir + "/.chronos"
	os.MkdirAll(configDir, 0o755)
	os.WriteFile(configDir+"/config.yaml", []byte("model: from-config\n"), 0o644)

	os.Setenv("CHRONOS_MODEL", "from-env")
	defer os.Unsetenv("CHRONOS_MODEL")

	got := envOrDefault("CHRONOS_MODEL", "default-val")
	if got != "from-env" {
		t.Errorf("envOrDefault should prefer env var over config, got %q", got)
	}
}

func TestExecuteConfigShow(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "config", "show"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "CHRONOS_MODEL") {
		t.Errorf("config show missing CHRONOS_MODEL: %q", output)
	}
	if !strings.Contains(output, "CHRONOS_DB_PATH") {
		t.Errorf("config show missing CHRONOS_DB_PATH: %q", output)
	}
}

func TestExecuteConfigSetMissingArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "config", "set", "model"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing value in config set")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage message, got: %v", err)
	}
}

func TestExecuteConfigModelShow(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "config", "model"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Current model:") {
		t.Errorf("expected model display, got: %q", output)
	}
}

func TestExecuteConfigModelSet(t *testing.T) {
	tmpDir := t.TempDir()
	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", oldHome)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "config", "model", "claude-3-opus"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Set model = claude-3-opus") {
		t.Errorf("expected confirmation, got: %q", output)
	}
}

func TestExecuteConfigUnknownSubcommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "config", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown config subcommand")
	}
	if !strings.Contains(err.Error(), "unknown config subcommand") {
		t.Errorf("expected 'unknown config subcommand' in error, got: %v", err)
	}
}

// --- P0-008: Sessions resume tests ---

func TestSessionsResume_NonResumableState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID: "completed-session", AgentID: "agent-1", Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	})

	output := captureStdout(t, func() {
		if err := sessionsResume(ctx, store, "completed-session"); err != nil {
			t.Fatalf("sessionsResume: %v", err)
		}
	})
	if !strings.Contains(output, "cannot be resumed") {
		t.Errorf("expected 'cannot be resumed' message, got: %q", output)
	}
}

func TestSessionsResume_SessionNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := sessionsResume(ctx, store, "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestSessionsResume_NoCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID: "paused-no-cp", AgentID: "agent-1", Status: "paused",
		CreatedAt: now, UpdatedAt: now,
	})

	err := sessionsResume(ctx, store, "paused-no-cp")
	if err == nil {
		t.Fatal("expected error when no checkpoint exists")
	}
	if !strings.Contains(err.Error(), "checkpoint") {
		t.Errorf("error should mention 'checkpoint', got: %v", err)
	}
}

func TestExecuteTeamCommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	t.Run("unknown subcommand", func(t *testing.T) {
		os.Args = []string{"chronos", "team", "bogus"}
		err := Execute()
		if err == nil {
			t.Fatal("expected error for unknown team subcommand")
		}
		if !strings.Contains(err.Error(), "unknown team subcommand") {
			t.Errorf("expected 'unknown team subcommand' in error, got: %v", err)
		}
	})

	t.Run("show missing id", func(t *testing.T) {
		os.Args = []string{"chronos", "team", "show"}
		err := Execute()
		if err == nil {
			t.Fatal("expected error for missing team ID")
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("expected 'usage' in error, got: %v", err)
		}
	})

	t.Run("run missing args", func(t *testing.T) {
		os.Args = []string{"chronos", "team", "run", "myteam"}
		err := Execute()
		if err == nil {
			t.Fatal("expected error for missing team run args")
		}
		if !strings.Contains(err.Error(), "usage") {
			t.Errorf("expected 'usage' in error, got: %v", err)
		}
	})
}
