package builtins

import (
	"context"
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
