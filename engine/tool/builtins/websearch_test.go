package builtins

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebSearch_MissingQuery(t *testing.T) {
	ws := NewWebSearchTool(5*time.Second, 5)
	_, err := ws.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestWebSearch_EmptyQuery(t *testing.T) {
	ws := NewWebSearchTool(5*time.Second, 5)
	_, err := ws.Handler(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearch_NonStringQuery(t *testing.T) {
	ws := NewWebSearchTool(0, 0)
	_, err := ws.Handler(context.Background(), map[string]any{"query": 123})
	if err == nil {
		t.Fatal("expected error for non-string query")
	}
}

func TestWebSearch_Definition(t *testing.T) {
	ws := NewWebSearchTool(0, 0)
	if ws.Name != "web_search" {
		t.Errorf("name = %q, want web_search", ws.Name)
	}
	if ws.Description == "" {
		t.Error("description should not be empty")
	}
}

func TestWebSearchCustom_MissingQuery(t *testing.T) {
	ws := NewWebSearchToolWithEngine("", 0)
	_, err := ws.Handler(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestWebSearchCustom_Definition(t *testing.T) {
	ws := NewWebSearchToolWithEngine("", 0)
	if ws.Name != "web_search_custom" {
		t.Errorf("name = %q, want web_search_custom", ws.Name)
	}
}

func TestWebSearchCustom_WithHTTPServer(t *testing.T) {
	testSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": "ok"}`)
	}))
	defer testSrv.Close()

	ws := NewWebSearchToolWithEngine(testSrv.URL+"/search?q=%s", 5*time.Second)
	result, err := ws.Handler(context.Background(), map[string]any{"query": "test query"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result")
	}
	if m["query"] != "test query" {
		t.Errorf("query=%v", m["query"])
	}
}

func TestWebSearchCustom_URLWithoutPercent(t *testing.T) {
	// When engine URL doesn't have %s, it should append ?q=%s
	ws := NewWebSearchToolWithEngine("http://localhost:12345/search", 0)
	if ws == nil {
		t.Fatal("expected non-nil definition")
	}
}

func TestNewWebSearchTool_WithHTTPServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"Abstract":"test abstract","AbstractSource":"Wikipedia","AbstractURL":"http://example.com","RelatedTopics":[{"Text":"topic 1","FirstURL":"http://example.com/1"}]}`)
	}))
	defer srv.Close()

	// Override the DuckDuckGo URL by using the WithEngine version
	ws := NewWebSearchToolWithEngine(srv.URL+"?q=%s", 5*time.Second)
	result, err := ws.Handler(context.Background(), map[string]any{"query": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
