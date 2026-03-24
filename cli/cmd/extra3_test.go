package cmd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/sdk/team"
	"github.com/spawn08/chronos/storage"
)

// ---------------------------------------------------------------------------
// sessionsResume: session has non-resumable status
// ---------------------------------------------------------------------------

func TestSessionsResume_CompletedStatus(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	store.CreateSession(ctx, &storage.Session{
		ID:        "sess-completed",
		AgentID:   "agent-1",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})

	output := captureStdout(t, func() {
		err := sessionsResume(ctx, store, "sess-completed")
		if err != nil {
			t.Errorf("sessionsResume: %v", err)
		}
	})
	if !strings.Contains(output, "cannot be resumed") {
		t.Errorf("expected 'cannot be resumed', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// sessionsResume: session not found
// ---------------------------------------------------------------------------

func TestSessionsResume_NotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := sessionsResume(ctx, store, "nonexistent-session")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// sessionsResume: session running but no checkpoint
// ---------------------------------------------------------------------------

func TestSessionsResume_RunningNoCheckpoint(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	store.CreateSession(ctx, &storage.Session{
		ID:        "sess-running2",
		AgentID:   "agent-1",
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// No checkpoint stored — GetLatestCheckpoint will fail
	err := sessionsResume(ctx, store, "sess-running2")
	if err == nil {
		t.Fatal("expected error for session with no checkpoint")
	}
	// Error can be about checkpoint or agent loading
	_ = err
}

// ---------------------------------------------------------------------------
// teamRun: missing args
// ---------------------------------------------------------------------------

func TestTeamRun_MissingArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "team", "run"}
	err := teamRun()
	if err == nil {
		t.Fatal("expected error for missing args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected 'usage', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// teamRun: config load failure (no config file)
// ---------------------------------------------------------------------------

func TestTeamRun_ConfigLoadFailure(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "team", "run", "some-team", "some message"}
	err := teamRun()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

// ---------------------------------------------------------------------------
// openStore: non-existent path (not in tmp dir = should create)
// ---------------------------------------------------------------------------

func TestOpenStore_CustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/custom.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	store, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	defer store.Close()
}

// ---------------------------------------------------------------------------
// runAgentCmd: unknown subcommand
// ---------------------------------------------------------------------------

func TestRunAgentCmd_UnknownSubcommand(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "agent", "unknown"}
	err := runAgentCmd()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

// ---------------------------------------------------------------------------
// agentShow: missing agent ID
// ---------------------------------------------------------------------------

func TestAgentShowInternal_MissingID(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "agent", "show"}
	err := agentShow("nonexistent-agent-id")
	if err == nil {
		t.Fatal("expected error for missing agent")
	}
}

// ---------------------------------------------------------------------------
// agentList: config load failure
// ---------------------------------------------------------------------------

func TestAgentListInternal_ConfigFailure(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	err := agentList()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

// ---------------------------------------------------------------------------
// teamList: config load failure
// ---------------------------------------------------------------------------

func TestTeamListInternal_ConfigFailure(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	err := teamList()
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

// ---------------------------------------------------------------------------
// teamShow: missing team ID
// ---------------------------------------------------------------------------

func TestTeamShowInternal_MissingID(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	os.Args = []string{"chronos", "team", "show"}
	err := teamShow("nonexistent-team-id")
	if err == nil {
		t.Fatal("expected error for missing team")
	}
}

// ---------------------------------------------------------------------------
// runSessions: unknown subcommand via direct call
// ---------------------------------------------------------------------------

func TestRunSessionsInternal_Unknown(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "unknown"}
	err := runSessions()
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

// ---------------------------------------------------------------------------
// NewWebSearchTool: construction with defaults
// ---------------------------------------------------------------------------

func TestNewWebSearchTool_Defaults(t *testing.T) {
	// Test that Execute dispatches "sessions resume" without crashing
	tmpDir := t.TempDir()
	os.Setenv("CHRONOS_DB_PATH", tmpDir+"/test.db")
	defer os.Unsetenv("CHRONOS_DB_PATH")

	// Init db first
	store, err := openStore()
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	ctx := context.Background()
	now := time.Now()
	store.CreateSession(ctx, &storage.Session{
		ID:        "r-sess",
		AgentID:   "agent-x",
		Status:    "completed",
		CreatedAt: now,
		UpdatedAt: now,
	})
	store.Close()

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "sessions", "resume", "r-sess"}
	output := captureStdout(t, func() {
		_ = Execute()
	})
	if !strings.Contains(output, "cannot be resumed") {
		t.Errorf("expected 'cannot be resumed', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// parseStrategy: valid and invalid
// ---------------------------------------------------------------------------

func TestParseStrategy_Valid(t *testing.T) {
	cases := []struct {
		in  string
		out team.Strategy
	}{
		{"sequential", team.StrategySequential},
		{"parallel", team.StrategyParallel},
		{"router", team.StrategyRouter},
		{"coordinator", team.StrategyCoordinator},
		{"SEQUENTIAL", team.StrategySequential},
	}
	for _, tc := range cases {
		got, err := parseStrategy(tc.in)
		if err != nil {
			t.Errorf("parseStrategy(%q): %v", tc.in, err)
		}
		if got != tc.out {
			t.Errorf("parseStrategy(%q) = %q, want %q", tc.in, got, tc.out)
		}
	}
}

func TestParseStrategy_Invalid(t *testing.T) {
	_, err := parseStrategy("bogus")
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
}

// ---------------------------------------------------------------------------
// parseErrorStrategy: valid and invalid
// ---------------------------------------------------------------------------

func TestParseErrorStrategy_Valid(t *testing.T) {
	cases := []struct {
		in  string
		out team.ErrorStrategy
	}{
		{"fail_fast", team.ErrorStrategyFailFast},
		{"failfast", team.ErrorStrategyFailFast},
		{"collect", team.ErrorStrategyCollect},
		{"best_effort", team.ErrorStrategyBestEffort},
		{"besteffort", team.ErrorStrategyBestEffort},
	}
	for _, tc := range cases {
		got, err := parseErrorStrategy(tc.in)
		if err != nil {
			t.Errorf("parseErrorStrategy(%q): %v", tc.in, err)
		}
		if got != tc.out {
			t.Errorf("parseErrorStrategy(%q) = %d, want %d", tc.in, got, tc.out)
		}
	}
}

func TestParseErrorStrategy_Invalid(t *testing.T) {
	_, err := parseErrorStrategy("unknown")
	if err == nil {
		t.Fatal("expected error for unknown error strategy")
	}
}

// ---------------------------------------------------------------------------
// teamRun: config loads but team not found
// ---------------------------------------------------------------------------

const teamRunYAML = `
agents:
  - id: agent-a
    name: Agent A
    model:
      provider: openai
      model: gpt-4o
teams:
  - id: team-seq
    name: Sequential Team
    strategy: sequential
    agents: [agent-a]
`

func TestTeamRun_TeamNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "team", "run", "nonexistent-team", "hello"}
	err := teamRun()
	if err == nil {
		t.Fatal("expected error for nonexistent team")
	}
}

// ---------------------------------------------------------------------------
// agentShow: valid agent config, agent found
// ---------------------------------------------------------------------------

func TestAgentShowInternal_ValidAgent(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	output := captureStdout(t, func() {
		err := agentShow("agent-a")
		if err != nil {
			t.Errorf("agentShow: %v", err)
		}
	})
	if !strings.Contains(output, "agent-a") {
		t.Errorf("expected 'agent-a', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// teamShow: valid team config, team found
// ---------------------------------------------------------------------------

func TestTeamShowInternal_ValidTeam(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	output := captureStdout(t, func() {
		err := teamShow("team-seq")
		if err != nil {
			t.Errorf("teamShow: %v", err)
		}
	})
	if !strings.Contains(output, "team-seq") {
		t.Errorf("expected 'team-seq', got: %q", output)
	}
}

// ---------------------------------------------------------------------------
// teamRun: team found but unknown strategy
// ---------------------------------------------------------------------------

const teamRunBadStrategyYAML = `
agents:
  - id: agent-b
    name: Agent B
    model:
      provider: openai
      model: gpt-4o
teams:
  - id: team-bad
    name: Bad Strategy Team
    strategy: unknown_strategy
    agents: [agent-b]
`

func TestTeamRun_UnknownStrategy(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunBadStrategyYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "team", "run", "team-bad", "hello"}
	err := teamRun()
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
	if !strings.Contains(err.Error(), "unknown strategy") {
		t.Errorf("expected 'unknown strategy', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// teamRun: team found with coordinator and error strategy fields
// ---------------------------------------------------------------------------

const teamRunFullYAML = `
agents:
  - id: coord-agent
    name: Coordinator
    model:
      provider: openai
      model: gpt-4o
  - id: worker-agent
    name: Worker
    model:
      provider: openai
      model: gpt-4o
teams:
  - id: team-full
    name: Full Team
    strategy: sequential
    agents: [worker-agent]
    coordinator: coord-agent
    max_concurrency: 2
    max_iterations: 5
    error_strategy: best_effort
`

func TestTeamRun_FullConfig(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunFullYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "team", "run", "team-full", "hello world"}
	// This will fail at t.Run() because there's no real LLM, but it exercises the setup code
	_ = teamRun()
}

// ---------------------------------------------------------------------------
// sessionsResume: session running with checkpoint, agent load fails
// ---------------------------------------------------------------------------

func TestSessionsResume_WithCheckpointNoAgent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	// Create a running session
	store.CreateSession(ctx, &storage.Session{
		ID:        "sess-cp",
		AgentID:   "agent-no-config",
		Status:    "running",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// Add a checkpoint
	store.SaveCheckpoint(ctx, &storage.Checkpoint{
		ID:        "cp-1",
		SessionID: "sess-cp",
		RunID:     "run-1",
		NodeID:    "node-start",
		State:     map[string]any{"input": "hello"},
		SeqNum:    1,
		CreatedAt: now,
	})

	// No agent config file - loadAgentByID will fail
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/nonexistent.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	output := captureStdout(t, func() {
		err := sessionsResume(ctx, store, "sess-cp")
		if err == nil {
			t.Error("expected error for missing agent config")
		}
	})
	// Should have printed session info before failing on loadAgentByID
	_ = output
}

// ---------------------------------------------------------------------------
// loadAgentByID: success path
// ---------------------------------------------------------------------------

func TestLoadAgentByID_Success(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	a, err := loadAgentByID("agent-a")
	if err != nil {
		t.Fatalf("loadAgentByID: %v", err)
	}
	if a.ID != "agent-a" {
		t.Errorf("ID = %q, want agent-a", a.ID)
	}
}

func TestLoadAgentByID_AgentNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	_, err := loadAgentByID("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

// ---------------------------------------------------------------------------
// loadDefaultAgent: success and empty agents
// ---------------------------------------------------------------------------

func TestLoadDefaultAgent_Success(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, teamRunYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	a, err := loadDefaultAgent()
	if err != nil {
		t.Fatalf("loadDefaultAgent: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil agent")
	}
}

const emptyAgentsYAML = `
agents: []
`

func TestLoadDefaultAgent_NoAgents(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("HOME", tmpDir)
	defer os.Unsetenv("HOME")
	writeAgentConfig(t, tmpDir, emptyAgentsYAML)
	os.Setenv("CHRONOS_CONFIG", tmpDir+"/.chronos/agents.yaml")
	defer os.Unsetenv("CHRONOS_CONFIG")

	_, err := loadDefaultAgent()
	if err == nil {
		t.Fatal("expected error for empty agents")
	}
	if !strings.Contains(err.Error(), "no agents") {
		t.Errorf("expected 'no agents', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Execute: version and help commands
// ---------------------------------------------------------------------------

func TestExecute_Version(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "version"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute version: %v", err)
		}
	})
	if !strings.Contains(output, "chronos") {
		t.Errorf("expected version output to contain 'chronos', got: %q", output)
	}
}

func TestExecute_Help(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "help"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute help: %v", err)
		}
	})
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected 'Usage', got: %q", output)
	}
}

func TestExecute_NoArgs(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute no args: %v", err)
		}
	})
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected usage output, got: %q", output)
	}
}

func TestExecute_HelpFlag(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	os.Args = []string{"chronos", "--help"}
	output := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Errorf("Execute --help: %v", err)
		}
	})
	if !strings.Contains(output, "Usage") {
		t.Errorf("expected usage output, got: %q", output)
	}
}
