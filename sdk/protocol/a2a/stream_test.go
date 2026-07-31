package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// slowEchoHandler waits briefly (so a running snapshot is observable) then echoes.
func slowEchoHandler(_ context.Context, task *Task) error {
	time.Sleep(40 * time.Millisecond)
	task.Output = "echo: " + task.Input
	return nil
}

func TestServerStreamTask(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, slowEchoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	created, err := c.CreateTask(ctx, "hi", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	tasks, errs := c.StreamTask(ctx, created.ID)
	snapshots := make([]Task, 0, 4)
	for tk := range tasks {
		snapshots = append(snapshots, tk)
	}
	if err := <-errs; err != nil {
		t.Fatalf("stream error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one streamed snapshot")
	}
	final := snapshots[len(snapshots)-1]
	if final.Status != TaskStatusCompleted {
		t.Errorf("final status: want completed, got %s", final.Status)
	}
	if final.Output != "echo: hi" {
		t.Errorf("final output: want %q, got %q", "echo: hi", final.Output)
	}
}

func TestStreamTaskNotFound(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	tasks, errs := c.StreamTask(context.Background(), "task_missing")
	for range tasks { //nolint:revive // drain
	}
	if err := <-errs; err == nil {
		t.Fatal("expected error streaming an unknown task")
	}
}

func TestRemoteAgentToolStreaming(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, slowEchoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	def := NewRemoteAgentTool("remote", "delegate", NewClient(srv.URL))
	out, err := def.Handler(context.Background(), map[string]any{"task": "world"})
	if err != nil {
		t.Fatalf("tool handler: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type %T", out)
	}
	if m["result"] != "echo: world" {
		t.Errorf("result: want %q, got %v", "echo: world", m["result"])
	}
}

// TestRemoteAgentToolPollFallback points the tool at a peer whose stream route
// is unavailable; the tool must fall back to polling and still return the result.
func TestRemoteAgentToolPollFallback(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	// Emulate a peer without the /stream endpoint.
	peer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/stream") {
			http.NotFound(w, r)
			return
		}
		s.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(peer)
	defer srv.Close()

	def := NewRemoteAgentTool("remote", "delegate", NewClient(srv.URL),
		WithPollInterval(10*time.Millisecond))
	out, err := def.Handler(context.Background(), map[string]any{"task": "ping"})
	if err != nil {
		t.Fatalf("tool handler: %v", err)
	}
	m := out.(map[string]any)
	if m["result"] != "echo: ping" {
		t.Errorf("result: want %q, got %v", "echo: ping", m["result"])
	}
}
