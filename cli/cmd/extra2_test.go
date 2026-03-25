package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Long description agent YAML (tests the >80 truncation path in agentShow)
const agentYAMLFull = `
agents:
  - id: agent-full
    name: Full Agent
    description: "This is a very detailed description that exceeds the normal display length for testing purposes"
    system: "You are a helpful assistant with very detailed system prompt that exceeds 80 characters limit test."
    instructions:
      - "Be concise"
      - "Use markdown"
    capabilities:
      - "web_search"
      - "code_execution"
    sub_agents:
      - "helper-agent"
    model:
      provider: openai
      model: gpt-4o
      base_url: "https://api.custom.example.com/v1"
    stream: true
    storage:
      backend: postgres
      dsn: "host=localhost port=5432 dbname=chronos user=admin password=secret sslmode=require extra_param=true more_params=yes"
`

func TestAgentShow_WithFullConfig(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAMLFull)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "agent", "show", "agent-full"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "agent-full") {
		t.Errorf("expected 'agent-full', got: %q", output)
	}
	if !strings.Contains(output, "Full Agent") {
		t.Errorf("expected 'Full Agent', got: %q", output)
	}
	// Should contain truncated or full description
	if !strings.Contains(output, "description") && !strings.Contains(output, "Description") {
		t.Errorf("expected description in output, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// runDB: status with existing file
// ---------------------------------------------------------------------------

func TestExecuteDB_StatusExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/existing.db"
	os.Setenv("CHRONOS_DB_PATH", dbPath)
	defer os.Unsetenv("CHRONOS_DB_PATH")

	// Create the DB via init first
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "db", "init"}
	captureStdout(t, func() { Execute() })

	// Now test status
	os.Args = []string{"chronos", "db", "status"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Database") {
		t.Errorf("expected 'Database', got: %q", output)
	}
	if !strings.Contains(output, "Size") {
		t.Errorf("expected 'Size', got: %q", output)
	}
	if !strings.Contains(output, "Sessions") {
		t.Errorf("expected 'Sessions', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// runMemory: forget path
// ---------------------------------------------------------------------------

func TestExecuteMemory_Forget(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	// Init the store
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()
	store.PutMemory(ctx, &storage.MemoryRecord{
		ID: "mem-to-forget", AgentID: "agent-1", Kind: "long_term",
		Key: "fact", Value: "test", CreatedAt: now,
	})

	// Use Execute with the test DB (separate from the in-memory store above)
	// We just test that the command path is reachable and the args validation works
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "forget", "some-memory-id"}
	// This will try to open the real store; just verify no panic and it runs
	_ = Execute()
}

// ---------------------------------------------------------------------------
// runMemory: clear path with agent ID
// ---------------------------------------------------------------------------

func TestExecuteMemory_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "clear", "agent-1"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Clearing all memories") {
		t.Errorf("expected 'Clearing all memories', got: %q", output)
	}
	if !strings.Contains(output, "Cleared") {
		t.Errorf("expected 'Cleared', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// runMemory: list with agent ID via Execute
// ---------------------------------------------------------------------------

func TestExecuteMemory_List(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "list", "agent-1"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "No memories found") {
		t.Errorf("expected 'No memories found', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// evalList: with eval files present
// ---------------------------------------------------------------------------

func TestEvalList_WithSuiteFiles(t *testing.T) {
	tmpDir := t.TempDir()
	evalDir := tmpDir + "/evals"
	os.MkdirAll(evalDir, 0o755)
	os.WriteFile(evalDir+"/my-suite.yaml", []byte("name: my-suite\n"), 0o644)

	old, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(old)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "eval", "list"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "my-suite.yaml") {
		t.Errorf("expected 'my-suite.yaml', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// teamShow: with coordinator and maxconcurrency
// ---------------------------------------------------------------------------

const teamYAMLWithCoordinator = `
agents:
  - id: coord
    name: Coordinator
    model:
      provider: openai
      model: gpt-4o
  - id: worker-a
    name: Worker A
    model:
      provider: openai
      model: gpt-4o

teams:
  - id: team-coord
    name: Coordinator Team
    strategy: coordinator
    agents: [worker-a]
    coordinator: coord
    max_concurrency: 4
    max_iterations: 10
    error_strategy: best_effort
`

func TestTeamShow_WithAllFields(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamYAMLWithCoordinator)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "show", "team-coord"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "team-coord") {
		t.Errorf("expected 'team-coord', got: %q", output)
	}
	if !strings.Contains(output, "coordinator") {
		t.Errorf("expected 'coordinator', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// sessions export via Execute
// ---------------------------------------------------------------------------

func TestExecuteSessionsExport(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	// Init the database first
	store, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID: "test-export-sess", AgentID: "agent-1", Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	})
	store.Close()

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "export", "test-export-sess"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "test-export-sess") {
		t.Errorf("expected session ID in output, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// sessions list with agent filter
// ---------------------------------------------------------------------------

func TestExecuteSessionsListWithAgent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	// Pre-populate
	store, _ := openStore()
	ctx := context.Background()
	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID: "ls1", AgentID: "my-agent", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	})
	store.Close()

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "list", "my-agent"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "ls1") {
		t.Errorf("expected 'ls1' in output, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// agentList with description truncation path
// ---------------------------------------------------------------------------

func TestAgentList_LongDescription(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAMLFull)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "agent", "list"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "agent-full") {
		t.Errorf("expected 'agent-full', got: %q", output)
	}
	// Description is truncated to 30 chars with "..."
	if !strings.Contains(output, "...") {
		t.Errorf("expected truncated description with '...', got: %q", output)
	}
}
