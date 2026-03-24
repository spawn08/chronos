// Package cmd provides the Chronos CLI command tree.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spawn08/chronos/cli/repl"
	"github.com/spawn08/chronos/engine/graph"
	chronosos "github.com/spawn08/chronos/os"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// Build-time variables set via -ldflags.
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// Execute runs the root CLI command.
func Execute() error {
	if len(os.Args) < 2 {
		return printUsage()
	}
	switch os.Args[1] {
	case "repl", "interactive":
		return runREPL()
	case "serve":
		return runServe()
	case "run":
		return runAgent()
	case "agent", "agents":
		return runAgentCmd()
	case "team", "teams":
		return runTeamCmd()
	case "sessions":
		return runSessions()
	case "memory":
		return runMemory()
	case "db":
		return runDB()
	case "eval", "evals":
		return runEvalCmd()
	case "config":
		return runConfig()
	case "version":
		return printVersion()
	case "help", "--help", "-h":
		return printUsage()
	default:
		return fmt.Errorf("unknown command: %s\nRun 'chronos help' for usage.", os.Args[1])
	}
}

func printUsage() error {
	fmt.Println(`Chronos CLI — Agentic Framework

Usage:
  chronos <command> [subcommand] [options]

Commands:
  repl                      Start interactive REPL (loads agent from YAML config)
  serve [addr]              Start ChronosOS control plane server (default :8420)
  run [--agent <id>] <msg>  Run an agent in headless mode
  agent list                List agents defined in config
  agent show <id>           Show agent configuration details
  agent chat <id>           Start a chat session with a specific agent
  team list                 List teams defined in config
  team run <id> <message>   Run a multi-agent team on a task
  team show <id>            Show team configuration details
  sessions                  Session management (list, resume, export)
  memory                    Memory management (list, forget, clear)
  db                        Database operations (init, status)
  eval list                 List available eval suites
  eval run <suite.yaml>     Run evaluation suite
  config                    Configuration (show)
  version                   Print version
  help                      Show this help

Agent Configuration:
  Define agents in .chronos/agents.yaml (project-level) or
  ~/.chronos/agents.yaml (global). See 'chronos agent list' for loaded agents.

Environment:
  CHRONOS_CONFIG    Path to agents YAML config file
  CHRONOS_DB_PATH   SQLite database path (default: chronos.db)
  CHRONOS_API_KEY   Default API key for model providers`)
	return nil
}

func printVersion() error {
	fmt.Printf("chronos %s\n", Version)
	fmt.Printf("  commit:    %s\n", Commit)
	fmt.Printf("  built:     %s\n", BuildDate)
	fmt.Printf("  go:        %s\n", runtime.Version())
	fmt.Printf("  os/arch:   %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

func openStore() (*sqlite.Store, error) {
	dbPath := os.Getenv("CHRONOS_DB_PATH")
	if dbPath == "" {
		dbPath = "chronos.db"
	}
	store, err := sqlite.New(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open storage: %w", err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		store.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return store, nil
}

// loadAgentConfig loads agent configuration from YAML,
// falling back to env-based defaults if no config file exists.
func loadAgentConfig() (*agent.FileConfig, error) {
	configPath := os.Getenv("CHRONOS_CONFIG")
	fc, err := agent.LoadFile(configPath)
	if err != nil {
		return nil, err
	}
	return fc, nil
}

// loadAgentByID loads a specific agent from YAML config by ID or name.
func loadAgentByID(idOrName string) (*agent.Agent, error) {
	fc, err := loadAgentConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := fc.FindAgent(idOrName)
	if err != nil {
		return nil, err
	}
	return agent.BuildAgent(context.Background(), cfg)
}

// loadDefaultAgent loads the first agent from YAML config.
func loadDefaultAgent() (*agent.Agent, error) {
	fc, err := loadAgentConfig()
	if err != nil {
		return nil, err
	}
	if len(fc.Agents) == 0 {
		return nil, fmt.Errorf("no agents defined in config")
	}
	return agent.BuildAgent(context.Background(), &fc.Agents[0])
}

func runREPL() error {
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()

	r := repl.New(store)

	// Try loading agent from YAML config for the REPL
	a, loadErr := loadDefaultAgent()
	if loadErr == nil && a != nil {
		r.SetAgent(a)
		fmt.Printf("Agent loaded: %s (%s)\n", a.Name, a.Model.Name())
	}

	return r.Start()
}

func runServe() error {
	addr := ":8420"
	if len(os.Args) > 2 {
		addr = os.Args[2]
	}
	store, err := openStore()
	if err != nil {
		return err
	}
	srv := chronosos.New(addr, store)
	log.Printf("Starting ChronosOS on %s", addr)
	return srv.Start(context.Background())
}

func runAgent() error {
	// Parse: chronos run [--agent <id>] <message...>
	args := os.Args[2:]
	agentID := ""
	var msgParts []string

	for i := 0; i < len(args); i++ {
		if (args[i] == "--agent" || args[i] == "-a") && i+1 < len(args) {
			agentID = args[i+1]
			i++
		} else {
			msgParts = append(msgParts, args[i])
		}
	}

	if len(msgParts) == 0 {
		return fmt.Errorf("usage: chronos run [--agent <id>] <message>")
	}
	message := strings.Join(msgParts, " ")

	var a *agent.Agent
	var err error
	if agentID != "" {
		a, err = loadAgentByID(agentID)
	} else {
		a, err = loadDefaultAgent()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not load agent from config: %v\n", err)
		fmt.Printf("Message: %s\n", message)
		fmt.Println("Create .chronos/agents.yaml to configure agents. Run 'chronos help' for details.")
		return nil
	}

	fmt.Printf("Agent: %s (model: %s)\n", a.Name, a.Model.Name())
	fmt.Printf("Message: %s\n\n", message)

	resp, err := a.Chat(context.Background(), message)
	if err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	fmt.Println(resp.Content)
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		fmt.Printf("\n[tokens: %d prompt + %d completion]\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	return nil
}

// --- agent subcommands ---

func runAgentCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return agentList()
	case "show":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos agent show <agent_id>")
		}
		return agentShow(os.Args[3])
	case "chat":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos agent chat <agent_id>")
		}
		return agentChat(os.Args[3])
	default:
		return fmt.Errorf("unknown agent subcommand: %s\nUsage: chronos agent [list|show|chat]", sub)
	}
}

func agentList() error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(fc.Agents) == 0 {
		fmt.Println("No agents defined.")
		return nil
	}
	fmt.Printf("%-15s %-20s %-15s %-15s %s\n", "ID", "NAME", "PROVIDER", "MODEL", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 85))
	for i := range fc.Agents {
		desc := fc.Agents[i].Description
		if len(desc) > 30 {
			desc = desc[:27] + "..."
		}
		modelName := fc.Agents[i].Model.Model
		if modelName == "" {
			modelName = "(default)"
		}
		fmt.Printf("%-15s %-20s %-15s %-15s %s\n", fc.Agents[i].ID, fc.Agents[i].Name, fc.Agents[i].Model.Provider, modelName, desc)
	}
	return nil
}

func agentShow(idOrName string) error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg, err := fc.FindAgent(idOrName)
	if err != nil {
		return err
	}

	fmt.Printf("Agent: %s\n", cfg.ID)
	fmt.Printf("  Name:          %s\n", cfg.Name)
	if cfg.Description != "" {
		fmt.Printf("  Description:   %s\n", cfg.Description)
	}
	fmt.Printf("  Provider:      %s\n", cfg.Model.Provider)
	fmt.Printf("  Model:         %s\n", cfg.Model.Model)
	if cfg.Model.BaseURL != "" {
		fmt.Printf("  Base URL:      %s\n", cfg.Model.BaseURL)
	}
	fmt.Printf("  Storage:       %s\n", storageLabel(cfg.Storage))
	if cfg.System != "" {
		prompt := cfg.System
		if len(prompt) > 80 {
			prompt = prompt[:77] + "..."
		}
		fmt.Printf("  System Prompt: %s\n", prompt)
	}
	if len(cfg.Instructions) > 0 {
		fmt.Printf("  Instructions:  %d\n", len(cfg.Instructions))
	}
	if len(cfg.Capabilities) > 0 {
		fmt.Printf("  Capabilities:  %s\n", strings.Join(cfg.Capabilities, ", "))
	}
	if len(cfg.SubAgents) > 0 {
		fmt.Printf("  Sub-agents:    %s\n", strings.Join(cfg.SubAgents, ", "))
	}
	if cfg.Stream {
		fmt.Printf("  Stream:        true\n")
	}
	return nil
}

func storageLabel(cfg agent.StorageConfig) string {
	if cfg.Backend == "" {
		return "sqlite (default)"
	}
	label := cfg.Backend
	if cfg.DSN != "" {
		dsn := cfg.DSN
		if len(dsn) > 40 {
			dsn = dsn[:37] + "..."
		}
		label += " (" + dsn + ")"
	}
	return label
}

func agentChat(idOrName string) error {
	a, err := loadAgentByID(idOrName)
	if err != nil {
		return err
	}

	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()

	r := repl.New(store)
	r.SetAgent(a)
	fmt.Printf("Chatting with agent: %s (%s / %s)\n", a.Name, a.Model.Name(), a.Model.Model())
	return r.Start()
}

// --- team subcommands ---

func runTeamCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return teamList()
	case "show":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos team show <team_id>")
		}
		return teamShow(os.Args[3])
	case "run":
		return teamRun()
	default:
		return fmt.Errorf("unknown team subcommand: %s\nUsage: chronos team [list|show|run]", sub)
	}
}

func teamList() error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(fc.Teams) == 0 {
		fmt.Println("No teams defined. Add a 'teams:' section to your agents.yaml.")
		return nil
	}
	fmt.Printf("%-15s %-20s %-15s %-10s %s\n", "ID", "NAME", "STRATEGY", "AGENTS", "COORDINATOR")
	fmt.Println(strings.Repeat("-", 80))
	for _, tc := range fc.Teams {
		coord := "-"
		if tc.Coordinator != "" {
			coord = tc.Coordinator
		}
		fmt.Printf("%-15s %-20s %-15s %-10d %s\n", tc.ID, tc.Name, tc.Strategy, len(tc.Agents), coord)
	}
	return nil
}

func teamShow(id string) error {
	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	tc, err := fc.FindTeam(id)
	if err != nil {
		return err
	}

	fmt.Printf("Team: %s\n", tc.ID)
	fmt.Printf("  Name:           %s\n", tc.Name)
	fmt.Printf("  Strategy:       %s\n", tc.Strategy)
	fmt.Printf("  Agents:         %s\n", strings.Join(tc.Agents, " → "))
	if tc.Coordinator != "" {
		fmt.Printf("  Coordinator:    %s\n", tc.Coordinator)
	}
	if tc.MaxConcurrency > 0 {
		fmt.Printf("  Max Concurrency: %d\n", tc.MaxConcurrency)
	}
	if tc.MaxIterations > 0 {
		fmt.Printf("  Max Iterations:  %d\n", tc.MaxIterations)
	}
	if tc.ErrorStrategy != "" {
		fmt.Printf("  Error Strategy:  %s\n", tc.ErrorStrategy)
	}
	return nil
}

func teamRun() error {
	args := os.Args[3:]
	if len(args) < 2 {
		return fmt.Errorf("usage: chronos team run <team_id> <message>")
	}
	teamID := args[0]
	message := strings.Join(args[1:], " ")

	ctx := context.Background()

	fc, err := loadAgentConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	tc, err := fc.FindTeam(teamID)
	if err != nil {
		return err
	}

	agents, err := agent.BuildAll(ctx, fc)
	if err != nil {
		return fmt.Errorf("build agents: %w", err)
	}

	strategy, err := parseStrategy(tc.Strategy)
	if err != nil {
		return err
	}

	t := team.New(tc.ID, tc.Name, strategy)

	for _, agentID := range tc.Agents {
		a, ok := agents[agentID]
		if !ok {
			return fmt.Errorf("team %q references unknown agent %q", tc.ID, agentID)
		}
		t.AddAgent(a)
	}

	if tc.Coordinator != "" {
		coord, ok := agents[tc.Coordinator]
		if !ok {
			return fmt.Errorf("team %q references unknown coordinator %q", tc.ID, tc.Coordinator)
		}
		t.SetCoordinator(coord)
	}
	if tc.MaxConcurrency > 0 {
		t.SetMaxConcurrency(tc.MaxConcurrency)
	}
	if tc.MaxIterations > 0 {
		t.SetMaxIterations(tc.MaxIterations)
	}
	if tc.ErrorStrategy != "" {
		es, esErr := parseErrorStrategy(tc.ErrorStrategy)
		if esErr != nil {
			return esErr
		}
		t.SetErrorStrategy(es)
	}

	fmt.Printf("Team: %s (%s strategy)\n", tc.Name, tc.Strategy)
	fmt.Printf("Agents: %s\n", strings.Join(tc.Agents, ", "))
	if tc.Coordinator != "" {
		fmt.Printf("Coordinator: %s\n", tc.Coordinator)
	}
	fmt.Printf("Message: %s\n\n", message)

	result, err := t.Run(ctx, graph.State{"message": message})
	if err != nil {
		return fmt.Errorf("team run: %w", err)
	}

	if resp, ok := result["response"]; ok {
		fmt.Println(resp)
	} else {
		for k, v := range result {
			if strings.HasPrefix(k, "_") {
				continue
			}
			fmt.Printf("%s: %v\n", k, v)
		}
	}

	history := t.MessageHistory()
	if len(history) > 0 {
		fmt.Printf("\n[%d inter-agent messages exchanged]\n", len(history))
	}
	return nil
}

func parseStrategy(s string) (team.Strategy, error) {
	switch strings.ToLower(s) {
	case "sequential":
		return team.StrategySequential, nil
	case "parallel":
		return team.StrategyParallel, nil
	case "router":
		return team.StrategyRouter, nil
	case "coordinator":
		return team.StrategyCoordinator, nil
	default:
		return "", fmt.Errorf("unknown strategy %q (supported: sequential, parallel, router, coordinator)", s)
	}
}

func parseErrorStrategy(s string) (team.ErrorStrategy, error) {
	switch strings.ToLower(s) {
	case "fail_fast", "failfast":
		return team.ErrorStrategyFailFast, nil
	case "collect":
		return team.ErrorStrategyCollect, nil
	case "best_effort", "besteffort":
		return team.ErrorStrategyBestEffort, nil
	default:
		return 0, fmt.Errorf("unknown error strategy %q (supported: fail_fast, collect, best_effort)", s)
	}
}

// --- eval subcommands ---

func runEvalCmd() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "list":
		return evalList()
	case "run":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos eval run <suite.yaml>")
		}
		return evalRun(os.Args[3])
	default:
		return fmt.Errorf("unknown eval subcommand: %s\nUsage: chronos eval [list|run]", sub)
	}
}

func evalList() error {
	patterns := []string{
		".chronos/evals/*.yaml",
		".chronos/evals/*.yml",
		"evals/*.yaml",
		"evals/*.yml",
	}
	found := false
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, m := range matches {
			fmt.Printf("  %s\n", m)
			found = true
		}
	}
	if !found {
		fmt.Println("No eval suites found.")
		fmt.Println("Place eval suite YAML files in .chronos/evals/ or evals/")
	}
	return nil
}

func evalRun(suitePath string) error {
	data, err := os.ReadFile(suitePath)
	if err != nil {
		return fmt.Errorf("read eval suite: %w", err)
	}
	fmt.Printf("Eval suite: %s (%d bytes)\n", suitePath, len(data))
	fmt.Println("Eval runner loaded. Define evals programmatically using the evals package.")
	fmt.Println("Suite YAML loading will be implemented with schema definition.")
	return nil
}

// --- sessions subcommands ---

func runSessions() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch sub {
	case "list":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		return sessionsList(ctx, store, agentID)
	case "resume":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos sessions resume <session_id>")
		}
		return sessionsResume(ctx, store, os.Args[3])
	case "export":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos sessions export <session_id>")
		}
		return sessionsExport(ctx, store, os.Args[3])
	default:
		return fmt.Errorf("unknown sessions subcommand: %s\nUsage: chronos sessions [list|resume|export]", sub)
	}
}

func sessionsList(ctx context.Context, store storage.Storage, agentID string) error {
	sessions, err := store.ListSessions(ctx, agentID, 20, 0)
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}
	fmt.Printf("%-30s %-15s %-12s %s\n", "ID", "AGENT", "STATUS", "CREATED")
	fmt.Println(strings.Repeat("-", 80))
	for _, s := range sessions {
		fmt.Printf("%-30s %-15s %-12s %s\n", s.ID, s.AgentID, s.Status, s.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

func sessionsResume(ctx context.Context, store storage.Storage, sessionID string) error {
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}
	if sess.Status != "running" && sess.Status != "paused" && sess.Status != "active" {
		fmt.Printf("Session %s is in state %q and cannot be resumed.\n", sessionID, sess.Status)
		return nil
	}

	cp, err := store.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("no checkpoint found for session %q: %w", sessionID, err)
	}

	fmt.Printf("Session: %s\n", sess.ID)
	fmt.Printf("Agent:   %s\n", sess.AgentID)
	fmt.Printf("Status:  %s\n", sess.Status)
	fmt.Printf("Checkpoint: node=%s seq=%d\n", cp.NodeID, cp.SeqNum)

	a, loadErr := loadAgentByID(sess.AgentID)
	if loadErr != nil {
		return fmt.Errorf("load agent %q: %w", sess.AgentID, loadErr)
	}

	a.Storage = store

	fmt.Println("\nResuming execution...")
	result, err := a.Resume(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("resume: %w", err)
	}

	fmt.Printf("\nStatus: %s\n", result.Status)
	if result.State != nil {
		stateJSON, _ := json.MarshalIndent(result.State, "", "  ")
		fmt.Printf("State:\n%s\n", string(stateJSON))
	}
	return nil
}

func sessionsExport(ctx context.Context, store storage.Storage, sessionID string) error {
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	events, err := store.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	fmt.Printf("# Session %s\n\n", sess.ID)
	fmt.Printf("- Agent: %s\n", sess.AgentID)
	fmt.Printf("- Status: %s\n", sess.Status)
	fmt.Printf("- Created: %s\n\n", sess.CreatedAt.Format(time.RFC3339))
	fmt.Printf("## Events (%d)\n\n", len(events))
	for _, e := range events {
		payload, _ := json.Marshal(e.Payload)
		fmt.Printf("- [seq=%d] %s: %s\n", e.SeqNum, e.Type, string(payload))
	}
	return nil
}

// --- memory subcommands ---

func runMemory() error {
	sub := "list"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()
	ctx := context.Background()

	switch sub {
	case "list":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		if agentID == "" {
			return fmt.Errorf("usage: chronos memory list <agent_id>")
		}
		return memoryList(ctx, store, agentID)
	case "forget":
		if len(os.Args) < 4 {
			return fmt.Errorf("usage: chronos memory forget <memory_id>")
		}
		return store.DeleteMemory(ctx, os.Args[3])
	case "clear":
		agentID := ""
		if len(os.Args) > 3 {
			agentID = os.Args[3]
		}
		if agentID == "" {
			return fmt.Errorf("usage: chronos memory clear <agent_id>")
		}
		fmt.Printf("Clearing all memories for agent %q\n", agentID)
		mems, err := store.ListMemory(ctx, agentID, "long_term")
		if err != nil {
			return err
		}
		for _, m := range mems {
			_ = store.DeleteMemory(ctx, m.ID)
		}
		fmt.Printf("Cleared %d memories.\n", len(mems))
		return nil
	default:
		return fmt.Errorf("unknown memory subcommand: %s\nUsage: chronos memory [list|forget|clear]", sub)
	}
}

func memoryList(ctx context.Context, store storage.Storage, agentID string) error {
	mems, err := store.ListMemory(ctx, agentID, "long_term")
	if err != nil {
		return fmt.Errorf("list memory: %w", err)
	}
	if len(mems) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	for _, m := range mems {
		fmt.Printf("  [%s] %s = %v\n", m.ID, m.Key, m.Value)
	}
	return nil
}

// --- db subcommands ---

func runDB() error {
	sub := "status"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "init":
		store, err := openStore()
		if err != nil {
			return err
		}
		defer store.Close()
		fmt.Println("Database initialized and migrated successfully.")
		return nil
	case "status":
		dbPath := os.Getenv("CHRONOS_DB_PATH")
		if dbPath == "" {
			dbPath = "chronos.db"
		}
		info, err := os.Stat(dbPath)
		if err != nil {
			fmt.Printf("Database: %s (not found)\n", dbPath)
			return nil
		}
		fmt.Printf("Database: %s\n", dbPath)
		fmt.Printf("Size: %s\n", humanizeBytes(info.Size()))
		fmt.Printf("Modified: %s\n", info.ModTime().Format(time.RFC3339))
		store, err := openStore()
		if err != nil {
			return nil
		}
		defer store.Close()
		sessions, _ := store.ListSessions(context.Background(), "", 1000, 0)
		fmt.Printf("Sessions: %d\n", len(sessions))
		return nil
	default:
		return fmt.Errorf("unknown db subcommand: %s\nUsage: chronos db [init|status]", sub)
	}
}

func humanizeBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// --- config subcommands ---

func runConfig() error {
	sub := "show"
	if len(os.Args) > 2 {
		sub = os.Args[2]
	}
	switch sub {
	case "show":
		fmt.Println("Chronos Configuration:")
		fmt.Printf("  CHRONOS_CONFIG:    %s\n", envOrDefault("CHRONOS_CONFIG", "(auto-detect)"))
		fmt.Printf("  CHRONOS_DB_PATH:   %s\n", envOrDefault("CHRONOS_DB_PATH", "chronos.db"))
		fmt.Printf("  CHRONOS_API_KEY:   %s\n", maskEnv("CHRONOS_API_KEY"))
		fmt.Printf("  CHRONOS_MODEL:     %s\n", envOrDefault("CHRONOS_MODEL", "gpt-4o"))
		fmt.Println()
		// Try to show loaded agents
		fc, err := loadAgentConfig()
		if err == nil && len(fc.Agents) > 0 {
			fmt.Printf("  Agents (%d):\n", len(fc.Agents))
			for j := range fc.Agents {
				fmt.Printf("    - %s (%s / %s)\n", fc.Agents[j].ID, fc.Agents[j].Model.Provider, fc.Agents[j].Model.Model)
			}
		} else {
			fmt.Println("  Agents: none (create .chronos/agents.yaml)")
		}
		return nil
	case "set":
		if len(os.Args) < 5 {
			return fmt.Errorf("usage: chronos config set <key> <value>")
		}
		return configSet(os.Args[3], os.Args[4])
	case "model":
		if len(os.Args) < 4 {
			fmt.Printf("Current model: %s\n", envOrDefault("CHRONOS_MODEL", "gpt-4o"))
			return nil
		}
		return configSet("model", os.Args[3])
	default:
		return fmt.Errorf("unknown config subcommand: %s\nUsage: chronos config [show|set|model]", sub)
	}
}

// configSet persists a key=value pair to ~/.chronos/config.yaml.
func configSet(key, value string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	configDir := filepath.Join(home, ".chronos")
	configPath := filepath.Join(configDir, "config.yaml")

	existing := make(map[string]string)
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				existing[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	existing[key] = value

	if mkErr := os.MkdirAll(configDir, 0o755); mkErr != nil {
		return fmt.Errorf("create config dir: %w", mkErr)
	}

	var buf strings.Builder
	buf.WriteString("# Chronos CLI configuration\n")
	for k, v := range existing {
		fmt.Fprintf(&buf, "%s: %s\n", k, v)
	}

	if writeErr := os.WriteFile(configPath, []byte(buf.String()), 0o644); writeErr != nil {
		return fmt.Errorf("write config: %w", writeErr)
	}

	fmt.Printf("Set %s = %s (saved to %s)\n", key, value, configPath)
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := configLookup(key); v != "" {
		return v
	}
	return def
}

// configLookup reads a value from ~/.chronos/config.yaml by key.
// It maps env-style keys (e.g. "CHRONOS_MODEL") to config-style keys
// (e.g. "model") and does a case-insensitive match.
func configLookup(key string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".chronos", "config.yaml"))
	if err != nil {
		return ""
	}

	normalized := strings.ToLower(strings.TrimPrefix(key, "CHRONOS_"))

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.ToLower(strings.TrimSpace(parts[0])) == normalized {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func maskEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		return "(not set)"
	}
	if len(v) <= 8 {
		return "****"
	}
	return v[:4] + "..." + v[len(v)-4:]
}
