package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// ---------------------------------------------------------------------------
// agentList / agentShow via Execute with a temp YAML config
// ---------------------------------------------------------------------------

func writeAgentConfig(t *testing.T, dir, content string) {
	t.Helper()
	chronosDir := dir + "/.chronos"
	if err := os.MkdirAll(chronosDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(chronosDir+"/agents.yaml", []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile agents.yaml: %v", err)
	}
}

const agentYAML = `
agents:
  - id: agent-1
    name: Test Agent
    description: A test agent for unit tests
    system: You are a helpful assistant.
    model:
      provider: openai
      model: gpt-4o
`

const teamYAML = `
agents:
  - id: agent-a
    name: Agent A
    model:
      provider: openai
      model: gpt-4o
  - id: agent-b
    name: Agent B
    model:
      provider: openai
      model: gpt-4o

teams:
  - id: team-1
    name: Test Team
    strategy: sequential
    agents: [agent-a, agent-b]
`

func TestAgentList_NoAgents(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, "agents: []\n")

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
	if !strings.Contains(output, "No agents defined") {
		t.Errorf("expected 'No agents defined', got: %q", output)
	}
}

func TestAgentList_WithAgents(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAML)

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
	if !strings.Contains(output, "agent-1") {
		t.Errorf("expected 'agent-1', got: %q", output)
	}
	if !strings.Contains(output, "Test Agent") {
		t.Errorf("expected 'Test Agent', got: %q", output)
	}
}

func TestAgentShow_Success(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "agent", "show", "agent-1"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "agent-1") {
		t.Errorf("expected 'agent-1', got: %q", output)
	}
	if !strings.Contains(output, "Test Agent") {
		t.Errorf("expected 'Test Agent', got: %q", output)
	}
}

func TestAgentShow_MissingID(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "agent", "show"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing agent ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage message, got: %v", err)
	}
}

func TestAgentShowUnknown(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "agent", "show", "nonexistent"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestAgentCmd_UnknownSubcommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "agent", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown agent subcommand")
	}
	if !strings.Contains(err.Error(), "unknown agent subcommand") {
		t.Errorf("expected 'unknown agent subcommand' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// teamList / teamShow
// ---------------------------------------------------------------------------

func TestTeamList_NoTeams(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, agentYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "list"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "No teams defined") {
		t.Errorf("expected 'No teams defined', got: %q", output)
	}
}

func TestTeamList_WithTeams(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "list"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "team-1") {
		t.Errorf("expected 'team-1', got: %q", output)
	}
}

func TestTeamShow_Success(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "show", "team-1"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "team-1") {
		t.Errorf("expected 'team-1', got: %q", output)
	}
	if !strings.Contains(output, "sequential") {
		t.Errorf("expected 'sequential' strategy, got: %q", output)
	}
}

func TestTeamShow_Unknown(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamYAML)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "show", "no-such-team"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent team")
	}
}

// ---------------------------------------------------------------------------
// eval subcommands
// ---------------------------------------------------------------------------

func TestEvalList_NoSuites(t *testing.T) {
	tmpDir := t.TempDir()
	// Change to a temp dir so no eval files exist
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
	if !strings.Contains(output, "No eval suites found") {
		t.Errorf("expected 'No eval suites found', got: %q", output)
	}
}

func TestEvalRun_FileNotFound(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "eval", "run", "/nonexistent/path/suite.yaml"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestEvalRun_Success(t *testing.T) {
	tmpDir := t.TempDir()
	suiteFile := tmpDir + "/suite.yaml"
	os.WriteFile(suiteFile, []byte("# eval suite\nname: my-suite\n"), 0o644)

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "eval", "run", suiteFile}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Eval suite") {
		t.Errorf("expected 'Eval suite', got: %q", output)
	}
}

func TestEvalCmd_UnknownSubcommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "eval", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown eval subcommand")
	}
	if !strings.Contains(err.Error(), "unknown eval subcommand") {
		t.Errorf("expected 'unknown eval subcommand', got: %v", err)
	}
}

func TestEvalCmd_RunMissingPath(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "eval", "run"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing eval suite path")
	}
}

// ---------------------------------------------------------------------------
// sessions subcommands via Execute
// ---------------------------------------------------------------------------

func TestExecuteSessions_UnknownSubcommand(t *testing.T) {
	// We need to set a real DB path to avoid openStore failing
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown sessions subcommand")
	}
	if !strings.Contains(err.Error(), "unknown sessions subcommand") {
		t.Errorf("expected 'unknown sessions subcommand', got: %v", err)
	}
}

func TestExecuteSessions_ResumeMissingID(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "resume"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing session ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

func TestExecuteSessions_ExportMissingID(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "export"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing export ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// memory subcommands via Execute
// ---------------------------------------------------------------------------

func TestExecuteMemory_UnknownSubcommand(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown memory subcommand")
	}
	if !strings.Contains(err.Error(), "unknown memory subcommand") {
		t.Errorf("expected 'unknown memory subcommand', got: %v", err)
	}
}

func TestExecuteMemory_ListMissingAgent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "list"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing agent ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

func TestExecuteMemory_ForgetMissingID(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "forget"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing memory ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

func TestExecuteMemory_ClearMissingAgent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "memory", "clear"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for missing agent ID")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

func TestMemoryClear_WithData(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	store.PutMemory(ctx, &storage.MemoryRecord{
		ID: "m1", AgentID: "agent-1", Kind: "long_term",
		Key: "fact", Value: "Alice", CreatedAt: now,
	})
	store.PutMemory(ctx, &storage.MemoryRecord{
		ID: "m2", AgentID: "agent-1", Kind: "long_term",
		Key: "fact2", Value: "Bob", CreatedAt: now,
	})

	// Set up to call runMemory via the store directly
	output := captureStdout(t, func() {
		mems, _ := store.ListMemory(ctx, "agent-1", "long_term")
		for _, m := range mems {
			store.DeleteMemory(ctx, m.ID)
		}
		if err := memoryList(ctx, store, "agent-1"); err != nil {
			t.Fatalf("memoryList: %v", err)
		}
	})
	if !strings.Contains(output, "No memories found") {
		t.Errorf("expected no memories after clear, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// db subcommands
// ---------------------------------------------------------------------------

func TestExecuteDB_Status(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "db", "status"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Database") {
		t.Errorf("expected 'Database', got: %q", output)
	}
}

func TestExecuteDB_Init(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "db", "init"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "Database initialized") {
		t.Errorf("expected 'Database initialized', got: %q", output)
	}
}

func TestExecuteDB_UnknownSubcommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "db", "bogus"}
	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown db subcommand")
	}
	if !strings.Contains(err.Error(), "unknown db subcommand") {
		t.Errorf("expected 'unknown db subcommand', got: %v", err)
	}
}

func TestExecuteDB_StatusNotFound(t *testing.T) {
	os.Setenv("CHRONOS_DB_PATH", "/nonexistent/path/to/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "db", "status"}
	output := captureStdout(t, func() {
		// may error or print not found
		Execute()
	})
	if !strings.Contains(output, "not found") && !strings.Contains(output, "Database") {
		t.Errorf("expected database not found message, got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// openStore
// ---------------------------------------------------------------------------

func TestOpenStore_ValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	store, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer store.Close()
}

func TestOpenStore_DefaultPath(t *testing.T) {
	os.Unsetenv("CHRONOS_DB_PATH")
	tmpDir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(old)

	store, err := openStore()
	if err != nil {
		t.Fatalf("openStore with default path: %v", err)
	}
	defer store.Close()
}

// ---------------------------------------------------------------------------
// sessions list via Execute
// ---------------------------------------------------------------------------

func TestExecuteSessionsList(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "list"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(output, "No sessions found") {
		t.Errorf("expected 'No sessions found', got: %q", output)
	}
}
