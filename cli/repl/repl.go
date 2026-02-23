// Package repl provides an interactive REPL for the Chronos CLI.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/chronos-ai/chronos/engine/model"
	"github.com/chronos-ai/chronos/sdk/agent"
	"github.com/chronos-ai/chronos/storage"
)

// REPL is the interactive command loop.
type REPL struct {
	store    storage.Storage
	agent    *agent.Agent
	commands map[string]Command
	history  []string
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
		commands: make(map[string]Command),
		ctx:      ctx,
		cancel:   cancel,
	}
	r.registerBuiltins()
	return r
}

// SetAgent configures the agent that handles non-command input.
func (r *REPL) SetAgent(a *agent.Agent) {
	r.agent = a
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
		Name: "/agent", Description: "Show current agent info",
		Handler: func(_ string) error {
			if r.agent == nil {
				fmt.Println("No agent loaded.")
				return nil
			}
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
		},
	})
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

// Ensure model is imported (used by SetAgent for type checking)
var _ model.Provider
