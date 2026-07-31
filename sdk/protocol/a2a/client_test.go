package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These tests cover the A2A client's error and edge-case paths and a few server
// route behaviors. (They replace the former _boost/_extra coverage-padding files
// with behavior-named tests, per plan/CONVENTIONS.md.)

func TestClient_NewClient_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.com/agent/")
	if c.baseURL != "http://example.com/agent" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestClient_GetAgentCard_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.GetAgentCard(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClient_GetAgentCard_RequestError(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // connection refused
	if _, err := c.GetAgentCard(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_CreateTask_DecodeErrorOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.CreateTask(context.Background(), "in", nil)
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestClient_CreateTask_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.CreateTask(context.Background(), "in", nil)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected HTTP 500 error, got %v", err)
	}
}

func TestClient_GetTask_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, err := c.GetTask(context.Background(), "task_1"); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClient_WaitForCompletion_DefaultPollInterval(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.CreateTask(context.Background(), "fast", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// pollInterval <= 0 normalizes to 1s inside WaitForCompletion.
	res, err := c.WaitForCompletion(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("WaitForCompletion: %v", err)
	}
	if res.Status != TaskStatusCompleted {
		t.Errorf("status = %s", res.Status)
	}
}

func TestServer_CancelUnknownID_Returns404(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/a2a/tasks/task_missing", http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestServer_CancelCompletedTask_StaysCompleted(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.CreateTask(context.Background(), "done", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Let the echo handler finish before canceling.
	if _, waitErr := c.WaitForCompletion(context.Background(), task.ID, 20*time.Millisecond); waitErr != nil {
		t.Fatalf("WaitForCompletion: %v", waitErr)
	}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		srv.URL+"/a2a/tasks/"+task.ID, http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var got Task
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != TaskStatusCompleted {
		t.Errorf("cancel on completed task: status = %s, want completed", got.Status)
	}
}

func TestServer_WrongMethodOnTasks_Returns404(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/a2a/tasks/task_1", http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestServer_EmptyTaskIDPath_Returns404(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/a2a/tasks/", http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for empty task id segment", w.Code)
	}
}
