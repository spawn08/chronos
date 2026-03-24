package builtins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPTool_GET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("X-Test", "yes")
		w.WriteHeader(200)
		w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	tool := NewHTTPTool(5*time.Second, 0)
	result, err := tool.Handler(context.Background(), map[string]any{
		"url": srv.URL,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["status_code"] != 200 {
		t.Errorf("status_code = %v", m["status_code"])
	}
	if m["body"] != "hello world" {
		t.Errorf("body = %q", m["body"])
	}
}

func TestHTTPTool_POST(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.WriteHeader(201)
		w.Write([]byte("created"))
	}))
	defer srv.Close()

	tool := NewHTTPTool(5*time.Second, 0)
	result, err := tool.Handler(context.Background(), map[string]any{
		"url":    srv.URL,
		"method": "post",
		"body":   `{"key": "value"}`,
		"headers": map[string]any{
			"Content-Type": "application/json",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["status_code"] != 201 {
		t.Errorf("status_code = %v", m["status_code"])
	}
}

func TestHTTPTool_MissingURL(t *testing.T) {
	tool := NewHTTPTool(5*time.Second, 0)
	_, err := tool.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestHTTPTool_InvalidURL(t *testing.T) {
	tool := NewHTTPTool(5*time.Second, 0)
	_, err := tool.Handler(context.Background(), map[string]any{
		"url": "://not-a-url",
	})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
