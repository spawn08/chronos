// Package repl provides an interactive REPL for the Chronos CLI.
package repl

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chronos-ai/chronos/storage"
)

// REPL is the interactive command loop.
type REPL struct {
	store    storage.Storage
	commands map[string]Command
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

func (r *REPL) registerBuiltins() {
	r.Register(Command{
		Name: "/help", Description: "Show available commands",
		Handler: func(_ string) error {
			fmt.Println("Available commands:")
			for _, c := range r.commands {
				fmt.Printf("  %-20s %s\n", c.Name, c.Description)
			}
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
				return fmt.Errorf("usage: /memory <agent_id>")
			}
			mems, err := r.store.ListMemory(r.ctx, agentID, "long_term")
			if err != nil {
				return err
			}
			for _, m := range mems {
				fmt.Printf("  %s = %v\n", m.Key, m.Value)
			}
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
	fmt.Println("Chronos REPL v0.1.0 — type /help for commands, /quit to exit")
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("chronos> ")
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
				fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmdName)
			}
			// Check if /quit was called
			select {
			case <-r.ctx.Done():
				fmt.Println("Goodbye.")
				return nil
			default:
			}
		} else {
			// Non-command input — future: send to agent
			fmt.Printf("Echo: %s\n", line)
		}
	}
	return scanner.Err()
}
