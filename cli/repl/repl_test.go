package repl

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

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

func TestNew(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	expectedCommands := []string{"/help", "/sessions", "/checkpoints", "/memory", "/history", "/clear", "/quit"}
	for _, cmd := range expectedCommands {
		if _, ok := r.commands[cmd]; !ok {
			t.Errorf("expected command %q to be registered", cmd)
		}
	}
}

func TestRegister(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	r.Register(Command{
		Name:        "/custom",
		Description: "A custom command",
		Handler:     func(_ string) error { return nil },
	})

	if _, ok := r.commands["/custom"]; !ok {
		t.Error("expected /custom to be registered")
	}
}

func TestSetAgent(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	a := &agent.Agent{
		ID:          "test-agent",
		Name:        "Test Agent",
		Description: "A test agent",
	}
	r.SetAgent(a)

	if r.agent != a {
		t.Error("expected agent to be set")
	}
	for _, cmd := range []string{"/model", "/agent"} {
		if _, ok := r.commands[cmd]; !ok {
			t.Errorf("expected command %q to be registered after SetAgent", cmd)
		}
	}
}

func TestSlashHelp(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	output := captureStdout(t, func() {
		if err := r.commands["/help"].Handler(""); err != nil {
			t.Fatalf("/help error: %v", err)
		}
	})
	if !strings.Contains(output, "Available commands") {
		t.Errorf("/help output missing 'Available commands': %q", output)
	}
	if !strings.Contains(output, "/quit") {
		t.Errorf("/help output missing '/quit': %q", output)
	}
}

func TestSlashHistory(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	t.Run("empty", func(t *testing.T) {
		output := captureStdout(t, func() {
			r.commands["/history"].Handler("")
		})
		if !strings.Contains(output, "No history") {
			t.Errorf("expected 'No history', got: %q", output)
		}
	})

	t.Run("with entries", func(t *testing.T) {
		r.history = append(r.history, "hello world", "how are you")
		output := captureStdout(t, func() {
			r.commands["/history"].Handler("")
		})
		if !strings.Contains(output, "hello world") || !strings.Contains(output, "how are you") {
			t.Errorf("history output missing entries: %q", output)
		}
	})
}

func TestSlashClear(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	r.history = append(r.history, "entry1", "entry2")
	captureStdout(t, func() {
		r.commands["/clear"].Handler("")
	})

	if len(r.history) != 0 {
		t.Errorf("expected history cleared, got %d entries", len(r.history))
	}
}

func TestSlashQuit(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	r.commands["/quit"].Handler("")

	select {
	case <-r.ctx.Done():
		// expected
	default:
		t.Error("expected context to be cancelled after /quit")
	}
}

func TestSlashSessions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		r := New(store)
		output := captureStdout(t, func() {
			r.commands["/sessions"].Handler("")
		})
		if !strings.Contains(output, "No sessions found") {
			t.Errorf("expected 'No sessions found', got: %q", output)
		}
	})

	t.Run("with sessions", func(t *testing.T) {
		now := time.Now()
		store.CreateSession(ctx, &storage.Session{
			ID: "repl-s1", AgentID: "", Status: "running",
			CreatedAt: now, UpdatedAt: now,
		})
		r := New(store)
		output := captureStdout(t, func() {
			r.commands["/sessions"].Handler("")
		})
		if !strings.Contains(output, "repl-s1") {
			t.Errorf("sessions output missing session ID: %q", output)
		}
	})
}

func TestSlashMemory(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("no agent no args", func(t *testing.T) {
		r := New(store)
		err := r.commands["/memory"].Handler("")
		if err == nil {
			t.Error("expected error when no agent ID provided")
		}
	})

	t.Run("with agent fallback", func(t *testing.T) {
		store.PutMemory(ctx, &storage.MemoryRecord{
			ID: "rm1", AgentID: "repl-agent", Kind: "long_term",
			Key: "pref", Value: "dark-mode",
		})

		r := New(store)
		r.agent = &agent.Agent{ID: "repl-agent", Name: "REPL Agent"}
		output := captureStdout(t, func() {
			if err := r.commands["/memory"].Handler(""); err != nil {
				t.Fatalf("/memory error: %v", err)
			}
		})
		if !strings.Contains(output, "pref") || !strings.Contains(output, "dark-mode") {
			t.Errorf("memory output missing data: %q", output)
		}
	})

	t.Run("empty memories", func(t *testing.T) {
		r := New(store)
		output := captureStdout(t, func() {
			r.commands["/memory"].Handler("nonexistent-agent")
		})
		if !strings.Contains(output, "No memories found") {
			t.Errorf("expected 'No memories found', got: %q", output)
		}
	})
}

func TestSlashCheckpoints(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	t.Run("missing session id", func(t *testing.T) {
		r := New(store)
		err := r.commands["/checkpoints"].Handler("")
		if err == nil {
			t.Error("expected error for missing session ID")
		}
	})

	t.Run("with checkpoints", func(t *testing.T) {
		store.SaveCheckpoint(ctx, &storage.Checkpoint{
			ID: "cp1", SessionID: "cp-sess", NodeID: "node-a",
			SeqNum: 1, State: map[string]any{}, CreatedAt: time.Now(),
		})

		r := New(store)
		output := captureStdout(t, func() {
			if err := r.commands["/checkpoints"].Handler("cp-sess"); err != nil {
				t.Fatalf("/checkpoints error: %v", err)
			}
		})
		if !strings.Contains(output, "cp1") || !strings.Contains(output, "node-a") {
			t.Errorf("checkpoints output missing data: %q", output)
		}
	})
}

func TestSlashModelNoAgent(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgent(&agent.Agent{ID: "test", Name: "Test"})

	output := captureStdout(t, func() {
		r.commands["/model"].Handler("")
	})
	if !strings.Contains(output, "No model configured") {
		t.Errorf("expected 'No model configured', got: %q", output)
	}
}

func TestSlashAgentInfo(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.SetAgent(&agent.Agent{
		ID:          "info-agent",
		Name:        "Info Agent",
		Description: "An informational agent",
	})

	output := captureStdout(t, func() {
		r.commands["/agent"].Handler("")
	})
	for _, want := range []string{"info-agent", "Info Agent", "An informational agent"} {
		if !strings.Contains(output, want) {
			t.Errorf("/agent output missing %q: %q", want, output)
		}
	}
}
