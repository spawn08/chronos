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

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	c := NewClient("http://example.com/agent/")
	if c.baseURL != "http://example.com/agent" {
		t.Errorf("baseURL = %q", c.baseURL)
	}
}

func TestClient_CreateTask_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.CreateTask(context.Background(), "in", nil)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status: %v", err)
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
	_, err := c.GetTask(context.Background(), "task_1")
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestServer_HandleCancelTask_CompletedNotCancelled(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	srv := httptest.NewServer(s)
	defer srv.Close()

	c := NewClient(srv.URL)
	task, err := c.CreateTask(context.Background(), "done", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	time.Sleep(80 * time.Millisecond)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		srv.URL+"/a2a/tasks/"+task.ID, nil)
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
		t.Errorf("cancel on completed task: status = %s", got.Status)
	}
}

func TestServer_ServeHTTP_WrongMethodOnTasks(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPatch, "/a2a/tasks/task_1", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestServer_HandleGetTask_EmptyIDPath(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/a2a/tasks/", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404 for empty task id segment", w.Code)
	}
}
