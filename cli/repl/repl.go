// Package repl provides an interactive REPL for the Chronos CLI.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
	"github.com/spawn08/chronos/storage"
)

// TeamRunner runs a configured team by ID and returns a stream of its events.
// The cmd package wires this in so the REPL can drive teams without importing
// the CLI's team-construction logic.
type TeamRunner func(ctx context.Context, teamID, message string) (<-chan team.TeamStreamEvent, error)

// REPL is the interactive command loop.
type REPL struct {
	store    storage.Storage
	agent    *agent.Agent            // the active agent handling chat input
	agents   map[string]*agent.Agent // full roster loaded from config
	order    []string                // agent IDs in config order, for stable listing
	teams    []string                // team IDs available to run
	runTeam  TeamRunner              // callback to execute a team (optional)
	commands map[string]Command
	history  []string
	stream   bool
	ctx      context.Context
	cancel   context.CancelFunc
}

// Command represents a slash command.
type Command struct {
	Name        string
	Description string
	Handler     func(args string) error
}

// New creates a new REPL with built-in commands.
func New(store storage.Storage) *REPL {
	ctx, cancel := context.WithCancel(context.Background())
	r := &REPL{
		store:    store,
		agents:   make(map[string]*agent.Agent),
		commands: make(map[string]Command),
		stream:   true,
		ctx:      ctx,
		cancel:   cancel,
	}
	r.registerBuiltins()
	return r
}

// SetStream enables or disables token-by-token streaming of agent responses.
func (r *REPL) SetStream(enabled bool) {
	r.stream = enabled
}

// SetAgent configures the single agent that handles non-command input. It also
// adds the agent to the roster so /agent can list it.
func (r *REPL) SetAgent(a *agent.Agent) {
	r.agent = a
	if a != nil {
		if _, exists := r.agents[a.ID]; !exists {
			r.order = append(r.order, a.ID)
		}
		r.agents[a.ID] = a
	}
	r.registerAgentCommands()
}

// SetAgents loads a full roster of agents (e.g. every agent in a YAML config).
// The first agent becomes the active one; use /agent <id> to switch.
func (r *REPL) SetAgents(agents []*agent.Agent) {
	for _, a := range agents {
		if a == nil {
			continue
		}
		if _, exists := r.agents[a.ID]; !exists {
			r.order = append(r.order, a.ID)
		}
		r.agents[a.ID] = a
		if r.agent == nil {
			r.agent = a
		}
	}
	r.registerAgentCommands()
}

// SetTeams registers the team IDs available to run and the callback that runs
// them. Enables the /teams and /team commands.
func (r *REPL) SetTeams(teamIDs []string, runner TeamRunner) {
	r.teams = teamIDs
	r.runTeam = runner
	r.registerAgentCommands()
}

// registerAgentCommands (re)registers the agent/model/team slash commands. Safe
// to call multiple times — handlers read live REPL state.
func (r *REPL) registerAgentCommands() {
	r.Register(Command{
		Name: "/model", Description: "Show current model info",
		Handler: func(_ string) error {
			if r.agent == nil || r.agent.Model == nil {
				fmt.Println("No model configured.")
				return nil
			}
			fmt.Printf("Provider: %s\n", r.agent.Model.Name())
			fmt.Printf("Model:    %s\n", r.agent.Model.Model())
			return nil
		},
	})
	r.Register(Command{
		Name: "/agent", Description: "List agents, show active, or /agent <id> to switch",
		Handler: r.handleAgentCommand,
	})
	if len(r.teams) > 0 || r.runTeam != nil {
		r.Register(Command{
			Name: "/teams", Description: "List teams defined in the config",
			Handler: func(_ string) error {
				if len(r.teams) == 0 {
					fmt.Println("No teams defined.")
					return nil
				}
				fmt.Printf("Teams (%d):\n", len(r.teams))
				for _, id := range r.teams {
					fmt.Printf("  %s\n", id)
				}
				return nil
			},
		})
		r.Register(Command{
			Name: "/team", Description: "Run a team: /team <id> <message>",
			Handler: r.handleTeamCommand,
		})
	}
}

// handleAgentCommand lists the roster (marking the active agent), switches the
// active agent when given an ID/name, or shows the active agent's details.
func (r *REPL) handleAgentCommand(args string) error {
	target := strings.TrimSpace(args)

	// Switch mode.
	if target != "" {
		a := r.agents[target]
		if a == nil {
			// Fall back to matching by name.
			for _, id := range r.order {
				if r.agents[id].Name == target {
					a = r.agents[id]
					break
				}
			}
		}
		if a == nil {
			return fmt.Errorf("unknown agent %q (use /agent to list)", target)
		}
		r.agent = a
		fmt.Printf("Switched to: %s (%s)\n", a.Name, a.ID)
		return nil
	}

	if r.agent == nil {
		fmt.Println("No agent loaded.")
		return nil
	}

	// List the roster when more than one agent is loaded.
	if len(r.order) > 1 {
		fmt.Printf("Agents (%d):\n", len(r.order))
		for _, id := range r.order {
			marker := "  "
			if id == r.agent.ID {
				marker = "* "
			}
			a := r.agents[id]
			if a.Name != "" && a.Name != a.ID {
				fmt.Printf("%s%s (%s)\n", marker, id, a.Name)
			} else {
				fmt.Printf("%s%s\n", marker, id)
			}
		}
		fmt.Println()
	}

	// Show the active agent's details.
	fmt.Printf("ID:          %s\n", r.agent.ID)
	fmt.Printf("Name:        %s\n", r.agent.Name)
	if r.agent.Description != "" {
		fmt.Printf("Description: %s\n", r.agent.Description)
	}
	if r.agent.Model != nil {
		fmt.Printf("Model:       %s / %s\n", r.agent.Model.Name(), r.agent.Model.Model())
	}
	if r.agent.SystemPrompt != "" {
		prompt := r.agent.SystemPrompt
		if len(prompt) > 100 {
			prompt = prompt[:97] + "..."
		}
		fmt.Printf("System:      %s\n", prompt)
	}
	return nil
}

// handleTeamCommand runs a configured team and streams its output.
func (r *REPL) handleTeamCommand(args string) error {
	if r.runTeam == nil {
		return fmt.Errorf("teams are not available in this session")
	}
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("usage: /team <id> <message>")
	}
	teamID := parts[0]
	message := strings.TrimSpace(parts[1])

	ch, err := r.runTeam(r.ctx, teamID, message)
	if err != nil {
		return err
	}

	fmt.Println()
	current := ""
	for evt := range ch {
		switch evt.Type {
		case team.TeamEventAgentStart:
			fmt.Printf("─── %s ───\n", evt.AgentID)
			current = evt.AgentID
		case team.TeamEventToken:
			if evt.AgentID != current {
				fmt.Printf("\n─── %s ───\n", evt.AgentID)
				current = evt.AgentID
			}
			fmt.Print(evt.Content)
		case team.TeamEventAgentEnd:
			fmt.Println()
		case team.TeamEventError:
			return fmt.Errorf("team run: %w", evt.Err)
		case team.TeamEventComplete:
			// Output already streamed.
		}
	}
	fmt.Println()
	return nil
}

func (r *REPL) registerBuiltins() {
	r.Register(Command{
		Name: "/help", Description: "Show available commands",
		Handler: func(_ string) error {
			fmt.Println("Available commands:")
			for _, c := range r.commands {
				fmt.Printf("  %-20s %s\n", c.Name, c.Description)
			}
			fmt.Println()
			fmt.Println("Prefixes:")
			fmt.Println("  !<cmd>     Run a shell command (e.g. ! ls -la)")
			fmt.Println("  /<cmd>     Run a slash command")
			fmt.Println("  <text>     Send message to agent")
			return nil
		},
	})
	r.Register(Command{
		Name: "/sessions", Description: "List recent sessions",
		Handler: func(_ string) error {
			sessions, err := r.store.ListSessions(r.ctx, "", 10, 0)
			if err != nil {
				return err
			}
			if len(sessions) == 0 {
				fmt.Println("No sessions found.")
				return nil
			}
			for _, s := range sessions {
				fmt.Printf("  [%s] %s  status=%s  agent=%s\n", s.CreatedAt.Format("2006-01-02 15:04"), s.ID, s.Status, s.AgentID)
			}
			return nil
		},
	})
	r.Register(Command{
		Name: "/checkpoints", Description: "List checkpoints for a session",
		Handler: func(args string) error {
			sessionID := strings.TrimSpace(args)
			if sessionID == "" {
				return fmt.Errorf("usage: /checkpoints <session_id>")
			}
			cps, err := r.store.ListCheckpoints(r.ctx, sessionID)
			if err != nil {
				return err
			}
			for _, cp := range cps {
				fmt.Printf("  [seq=%d] %s  node=%s\n", cp.SeqNum, cp.ID, cp.NodeID)
			}
			return nil
		},
	})
	r.Register(Command{
		Name: "/memory", Description: "List long-term memories for an agent",
		Handler: func(args string) error {
			agentID := strings.TrimSpace(args)
			if agentID == "" {
				if r.agent != nil {
					agentID = r.agent.ID
				} else {
					return fmt.Errorf("usage: /memory <agent_id>")
				}
			}
			mems, err := r.store.ListMemory(r.ctx, agentID, "long_term")
			if err != nil {
				return err
			}
			if len(mems) == 0 {
				fmt.Println("No memories found.")
				return nil
			}
			for _, m := range mems {
				fmt.Printf("  %s = %v\n", m.Key, m.Value)
			}
			return nil
		},
	})
	r.Register(Command{
		Name: "/history", Description: "Show conversation history for this session",
		Handler: func(_ string) error {
			if len(r.history) == 0 {
				fmt.Println("No history yet.")
				return nil
			}
			for i, h := range r.history {
				fmt.Printf("  %d: %s\n", i+1, h)
			}
			return nil
		},
	})
	r.Register(Command{
		Name: "/stream", Description: "Toggle streaming responses (on|off), or show current state",
		Handler: func(args string) error {
			switch strings.ToLower(strings.TrimSpace(args)) {
			case "on", "true", "1":
				r.stream = true
			case "off", "false", "0":
				r.stream = false
			case "":
				// no-op: just report current state below
			default:
				return fmt.Errorf("usage: /stream [on|off]")
			}
			state := "off"
			if r.stream {
				state = "on"
			}
			fmt.Printf("Streaming is %s.\n", state)
			return nil
		},
	})
	r.Register(Command{
		Name: "/clear", Description: "Clear conversation history",
		Handler: func(_ string) error {
			r.history = nil
			fmt.Println("History cleared.")
			return nil
		},
	})
	r.Register(Command{
		Name: "/quit", Description: "Exit the REPL",
		Handler: func(_ string) error {
			r.cancel()
			return nil
		},
	})
}

// Register adds a slash command.
func (r *REPL) Register(c Command) {
	r.commands[c.Name] = c
}

// Start begins the interactive loop.
func (r *REPL) Start() error {
	label := "Chronos REPL v0.1.0"
	if r.agent != nil {
		label += fmt.Sprintf(" [%s]", r.agent.Name)
	}
	fmt.Printf("%s — type /help for commands, /quit to exit\n", label)

	scanner := bufio.NewScanner(os.Stdin)
	// Allow long input lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		prompt := "chronos> "
		if r.agent != nil {
			prompt = r.agent.Name + "> "
		}
		fmt.Print(prompt)
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		select {
		case <-r.ctx.Done():
			fmt.Println("Goodbye.")
			return nil
		default:
		}

		// Shell escape: ! prefix
		if strings.HasPrefix(line, "!") {
			shellCmd := strings.TrimSpace(line[1:])
			if shellCmd != "" {
				r.execShell(shellCmd)
			}
			continue
		}

		// Slash commands: / prefix
		if strings.HasPrefix(line, "/") {
			parts := strings.SplitN(line, " ", 2)
			cmdName := parts[0]
			args := ""
			if len(parts) > 1 {
				args = parts[1]
			}
			if cmd, ok := r.commands[cmdName]; ok {
				if err := cmd.Handler(args); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				}
			} else {
				fmt.Fprintf(os.Stderr, "Unknown command: %s (type /help for list)\n", cmdName)
			}
			select {
			case <-r.ctx.Done():
				fmt.Println("Goodbye.")
				return nil
			default:
			}
			continue
		}

		// Agent chat
		r.history = append(r.history, line)
		if r.agent != nil && r.agent.Model != nil {
			r.chatWithAgent(line)
		} else {
			fmt.Println("No agent loaded. Create .chronos/agents.yaml or use 'chronos agent chat <id>'.")
		}
	}
	return scanner.Err()
}

func (r *REPL) chatWithAgent(message string) {
	if r.stream {
		r.chatStream(message)
		return
	}
	resp, err := r.agent.Chat(r.ctx, message)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	fmt.Println()
	fmt.Println(resp.Content)
	fmt.Println()
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		fmt.Printf("[tokens: %d prompt + %d completion]\n", resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
}

// chatStream prints the agent's response incrementally as tokens arrive.
func (r *REPL) chatStream(message string) {
	ch, err := r.agent.ChatStream(r.ctx, message)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	fmt.Println()
	var usage model.Usage
	var printed bool
	for chunk := range ch {
		if chunk.Err != nil {
			fmt.Fprintf(os.Stderr, "\nError: %v\n", chunk.Err)
			return
		}
		if chunk.Delta {
			fmt.Print(chunk.Content)
			printed = true
			continue
		}
		// Final summary chunk carries usage totals.
		usage = chunk.Usage
	}
	if printed {
		fmt.Println()
	}
	fmt.Println()
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		fmt.Printf("[tokens: %d prompt + %d completion]\n", usage.PromptTokens, usage.CompletionTokens)
	}
}

func (r *REPL) execShell(cmdStr string) {
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return
	}
	cmd := exec.CommandContext(r.ctx, parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Shell error: %v\n", err)
	}
}
