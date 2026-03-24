package builtins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileReadTool(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("hello"), 0644)

	tool := NewFileReadTool(dir)
	result, err := tool.Handler(context.Background(), map[string]any{"path": "test.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["content"] != "hello" {
		t.Errorf("content = %q", m["content"])
	}
}

func TestFileReadTool_Missing(t *testing.T) {
	tool := NewFileReadTool(t.TempDir())
	_, err := tool.Handler(context.Background(), map[string]any{"path": "nope.txt"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileWriteTool(t *testing.T) {
	dir := t.TempDir()
	tool := NewFileWriteTool(dir)
	_, err := tool.Handler(context.Background(), map[string]any{
		"path":    "sub/out.txt",
		"content": "world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "sub", "out.txt"))
	if string(data) != "world" {
		t.Errorf("wrote %q", data)
	}
}

func TestFileListTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)

	tool := NewFileListTool("")
	result, err := tool.Handler(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	entries := m["entries"].([]map[string]any)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestFileGlobTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(""), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte(""), 0644)

	tool := NewFileGlobTool(dir)
	result, err := tool.Handler(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	matches := m["matches"].([]string)
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}
}

func TestFileGrepTool(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("line one\nline two\nline three"), 0644)

	tool := NewFileGrepTool(dir)
	result, err := tool.Handler(context.Background(), map[string]any{
		"path":    "test.txt",
		"pattern": "two",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	matches := m["matches"].([]map[string]any)
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	if matches[0]["line_number"] != 2 {
		t.Errorf("line_number = %v", matches[0]["line_number"])
	}
}

func TestFileToolkit(t *testing.T) {
	tk := NewFileToolkit(t.TempDir())
	if len(tk.Tools) != 5 {
		t.Errorf("expected 5 tools, got %d", len(tk.Tools))
	}
}
