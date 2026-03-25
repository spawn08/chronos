package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func echoHandler(ctx context.Context, task *Task) error {
	task.Output = "echo: " + task.Input
	return nil
}

func failHandler(ctx context.Context, task *Task) error {
	return fmt.Errorf("handler failed")
}

func TestNewServer(t *testing.T) {
	card := AgentCard{Name: "test-agent", Version: "1.0", Capabilities: []string{"chat"}}
	s := NewServer(card, echoHandler)
	if s == nil {
		t.Fatal("NewServer returned nil")
	}
}

func TestGetAgentCard(t *testing.T) {
	card := AgentCard{
		Name:         "my-agent",
		Description:  "A test agent",
		Version:      "2.0",
		Capabilities: []string{"search", "code"},
	}
	s := NewServer(card, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	got, err := c.GetAgentCard(context.Background())
	if err != nil {
		t.Fatalf("GetAgentCard failed: %v", err)
	}
	if got.Name != card.Name {
		t.Errorf("Name: expected %q, got %q", card.Name, got.Name)
	}
	if got.Version != card.Version {
		t.Errorf("Version: expected %q, got %q", card.Version, got.Version)
	}
}

func TestCreateTask(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.CreateTask(context.Background(), "hello world", nil)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if task.ID == "" {
		t.Fatal("task ID is empty")
	}
	if task.Input != "hello world" {
		t.Errorf("expected input 'hello world', got %q", task.Input)
	}
	validStatuses := map[TaskStatus]bool{
		TaskStatusPending:   true,
		TaskStatusRunning:   true,
		TaskStatusCompleted: true,
	}
	if !validStatuses[task.Status] {
		t.Errorf("unexpected status: %s", task.Status)
	}
}

func TestCreateTaskWithMetadata(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	meta := map[string]any{"key": "value"}
	task, err := c.CreateTask(context.Background(), "test", meta)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if task.Metadata == nil {
		t.Error("expected metadata, got nil")
	}
}

func TestGetTask(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	created, err := c.CreateTask(context.Background(), "ping", nil)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	got, err := c.GetTask(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: expected %q, got %q", created.ID, got.ID)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetTask(context.Background(), "task_9999")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestWaitForCompletion(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.CreateTask(context.Background(), "compute", nil)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.WaitForCompletion(ctx, task.ID, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForCompletion failed: %v", err)
	}
	if result.Status != TaskStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if result.Output != "echo: compute" {
		t.Errorf("unexpected output: %s", result.Output)
	}
}

func TestWaitForCompletionFailed(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, failHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, _ := c.CreateTask(context.Background(), "fail", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := c.WaitForCompletion(ctx, task.ID, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForCompletion failed: %v", err)
	}
	if result.Status != TaskStatusFailed {
		t.Errorf("expected failed, got %s", result.Status)
	}
}

func TestCancelTask(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, func(ctx context.Context, task *Task) error {
		time.Sleep(2 * time.Second) // slow handler
		return nil
	})
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, _ := c.CreateTask(context.Background(), "slow", nil)

	// Cancel via direct HTTP call
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		srv.URL+"/a2a/tasks/"+task.ID, http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cancel request failed: %v", err)
	}
	defer resp.Body.Close()
	// Should be 200 or 404 depending on timing
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("unexpected status: %d", resp.StatusCode)
	}
}

func TestServeHTTPUnknownRoute(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/a2a/unknown", http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestCreateTaskBadJSON(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/a2a/tasks", bytes.NewBufferString("bad json"))
	s.ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTaskStatusConstants(t *testing.T) {
	statuses := []TaskStatus{
		TaskStatusPending,
		TaskStatusRunning,
		TaskStatusCompleted,
		TaskStatusFailed,
		TaskStatusCancelled,
	}
	seen := map[TaskStatus]bool{}
	for _, s := range statuses {
		if seen[s] {
			t.Errorf("duplicate status: %s", s)
		}
		seen[s] = true
		if s == "" {
			t.Error("empty status")
		}
	}
}

func TestMultipleTasksSequential(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	for i := 0; i < 5; i++ {
		task, err := c.CreateTask(context.Background(), fmt.Sprintf("msg-%d", i), nil)
		if err != nil {
			t.Fatalf("CreateTask %d failed: %v", i, err)
		}
		if task.ID == "" {
			t.Errorf("task %d has empty ID", i)
		}
	}
}

func TestAgentCardJSONSerialization(t *testing.T) {
	card := AgentCard{
		Name:         "test",
		Description:  "desc",
		Version:      "1.0",
		Capabilities: []string{"chat"},
	}
	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var got AgentCard
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.Name != card.Name {
		t.Errorf("Name mismatch")
	}
}

func TestWaitForCompletionContextCancelled(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, func(ctx context.Context, task *Task) error {
		time.Sleep(5 * time.Second)
		return nil
	})
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, _ := c.CreateTask(context.Background(), "slow", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.WaitForCompletion(ctx, task.ID, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}
