package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClient_GetAgentCard_InvalidJSON_Boost(t *testing.T) {
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

func TestClient_CreateTask_DecodeErrorOnSuccess_Boost(t *testing.T) {
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

func TestClient_GetAgentCard_RequestError_Boost(t *testing.T) {
	c := NewClient("http://127.0.0.1:1") // connection refused
	_, err := c.GetAgentCard(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_WaitForCompletion_DefaultPollInterval_Boost(t *testing.T) {
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

	// pollInterval <= 0 should normalize to 1s inside WaitForCompletion
	res, err := c.WaitForCompletion(ctx, task.ID, 0)
	if err != nil {
		t.Fatalf("WaitForCompletion: %v", err)
	}
	if res.Status != TaskStatusCompleted {
		t.Errorf("status = %s", res.Status)
	}
}

func TestServer_HandleCancelTask_UnknownID_Boost(t *testing.T) {
	s := NewServer(AgentCard{Name: "agent"}, echoHandler)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/a2a/tasks/task_missing", http.NoBody)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
