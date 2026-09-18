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
	os.WriteFile(f, []byte("hello"), 0o644)

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
		return
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
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0o644)

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
	os.WriteFile(filepath.Join(dir, "a.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte(""), 0o644)

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
	os.WriteFile(f, []byte("line one\nline two\nline three"), 0o644)

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

func TestFileWriteTool_NoPath(t *testing.T) {
	tool := NewFileWriteTool(t.TempDir())
	_, err := tool.Handler(context.Background(), map[string]any{"content": "data"})
	if err == nil {
		t.Fatal("expected error for missing path")
		return
	}
}

func TestFileWriteTool_Subdirectory(t *testing.T) {
	dir := t.TempDir()
	tool := NewFileWriteTool(dir)
	_, err := tool.Handler(context.Background(), map[string]any{
		"path":    "subdir/file.txt",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileGlobTool_NoPattern(t *testing.T) {
	tool := NewFileGlobTool(t.TempDir())
	_, err := tool.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing pattern")
		return
	}
}

func TestFileGlobTool_InvalidPattern(t *testing.T) {
	tool := NewFileGlobTool(t.TempDir())
	_, err := tool.Handler(context.Background(), map[string]any{"pattern": "["})
	if err == nil {
		t.Log("some systems allow this pattern without error")
	}
}

func TestFileListTool_InvalidDir(t *testing.T) {
	tool := NewFileListTool("/nonexistent-dir-xyz")
	_, err := tool.Handler(context.Background(), map[string]any{"path": "."})
	if err == nil {
		t.Fatal("expected error for invalid base path")
		return
	}
}

func TestResolvePath(t *testing.T) {
	tests := []struct {
		base     string
		rel      string
		expected string
	}{
		{"/tmp", "file.txt", "/tmp/file.txt"},
		{"/tmp", "/abs/path.txt", "/abs/path.txt"},
		{"/tmp", "sub/dir/file.txt", "/tmp/sub/dir/file.txt"},
	}
	for _, tt := range tests {
		got := resolvePath(tt.base, tt.rel)
		if got != tt.expected {
			t.Errorf("resolvePath(%q, %q) = %q, want %q", tt.base, tt.rel, got, tt.expected)
		}
	}
}

func TestFileToolsUseRequestWorkspaceWithoutChangingConfiguredBase(t *testing.T) {
	configured := t.TempDir()
	requestRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(configured, "value.txt"), []byte("configured"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(requestRoot, "value.txt"), []byte("request"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configured, "configured-only.go"), []byte("configured marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(requestRoot, "request-only.go"), []byte("request marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	readTool := NewFileReadTool(configured)
	ctx := WithWorkspaceRoot(context.Background(), requestRoot)
	result, err := readTool.Handler(ctx, map[string]any{"path": "value.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(map[string]any)["content"]; got != "request" {
		t.Fatalf("request-scoped content = %q, want request", got)
	}
	result, err = readTool.Handler(context.Background(), map[string]any{"path": "value.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(map[string]any)["content"]; got != "configured" {
		t.Fatalf("configured content = %q, want configured", got)
	}
	listResult, err := NewFileListTool(configured).Handler(ctx, map[string]any{"path": "."})
	if err != nil {
		t.Fatal(err)
	}
	entries := listResult.(map[string]any)["entries"].([]map[string]any)
	if len(entries) != 2 || entries[0]["name"] != "request-only.go" && entries[1]["name"] != "request-only.go" {
		t.Fatalf("request-scoped list = %#v", entries)
	}
	globResult, err := NewFileGlobTool(configured).Handler(ctx, map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if matches := globResult.(map[string]any)["matches"].([]string); len(matches) != 1 || filepath.Base(matches[0]) != "request-only.go" {
		t.Fatalf("request-scoped glob = %#v", matches)
	}
	grepResult, err := NewFileGrepTool(configured).Handler(ctx, map[string]any{"path": "request-only.go", "pattern": "request marker"})
	if err != nil {
		t.Fatal(err)
	}
	if matches := grepResult.(map[string]any)["matches"].([]map[string]any); len(matches) != 1 {
		t.Fatalf("request-scoped grep = %#v", matches)
	}
}

func TestFileWriteRemapsConfiguredAbsolutePathAndRejectsOutsideRequestWorkspace(t *testing.T) {
	configured := t.TempDir()
	requestRoot := t.TempDir()
	ctx := WithWorkspaceRoot(context.Background(), requestRoot)
	writeTool := NewFileWriteTool(configured)
	configuredPath := filepath.Join(configured, "nested", "value.txt")
	if _, err := writeTool.Handler(ctx, map[string]any{"path": configuredPath, "content": "isolated"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(requestRoot, "nested", "value.txt")); err != nil || string(data) != "isolated" {
		t.Fatalf("isolated file = %q, %v", data, err)
	}
	if _, err := os.Stat(configuredPath); !os.IsNotExist(err) {
		t.Fatalf("configured workspace was modified: %v", err)
	}
	if _, err := writeTool.Handler(ctx, map[string]any{"path": filepath.Join(t.TempDir(), "outside.txt"), "content": "bad"}); err == nil {
		t.Fatal("outside absolute path was accepted")
	}
}
