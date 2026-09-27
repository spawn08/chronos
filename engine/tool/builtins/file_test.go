package builtins

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	enginetool "github.com/spawn08/chronos/engine/tool"
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

func TestFileWriteEffectSeparatesScratchFromDelivery(t *testing.T) {
	dir := t.TempDir()
	registry := enginetool.NewRegistry()
	registry.Register(NewFileWriteTool(dir))
	if err := registry.SetPermissionMode(enginetool.PermissionModeAutoApprove); err != nil {
		t.Fatal(err)
	}
	scratchCtx := enginetool.WithEffectGrant(enginetool.WithScratchWorkspace(context.Background()), enginetool.EffectScratchWrite)
	if _, err := registry.Execute(scratchCtx, "file_write", map[string]any{"path": "review.txt", "content": "candidate"}); err != nil {
		t.Fatalf("scratch write: %v", err)
	}
	if _, err := registry.Execute(enginetool.WithEffectGrant(context.Background(), enginetool.EffectScratchWrite), "file_write", map[string]any{"path": "delivery.txt", "content": "forbidden"}); err == nil {
		t.Fatal("scratch-only grant wrote to delivery workspace")
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

func TestFileReadRejectsSymlinkOutsideRequestWorkspace(t *testing.T) {
	parent := t.TempDir()
	requestRoot := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside.txt")
	if err := os.Mkdir(requestRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(requestRoot, "escape.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := NewFileReadTool(requestRoot).Handler(
		WithWorkspaceRoot(context.Background(), requestRoot),
		map[string]any{"path": "escape.txt"},
	)
	if err == nil {
		t.Fatal("read through symlink outside request workspace was accepted")
	}
}

func TestFileWriteRejectsSymlinkOutsideRequestWorkspace(t *testing.T) {
	parent := t.TempDir()
	requestRoot := filepath.Join(parent, "workspace")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(requestRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(requestRoot, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	outsideFile := filepath.Join(outside, "written.txt")
	_, err := NewFileWriteTool(requestRoot).Handler(
		WithWorkspaceRoot(context.Background(), requestRoot),
		map[string]any{"path": filepath.Join("escape", "written.txt"), "content": "bad"},
	)
	if err == nil {
		t.Fatal("write through symlink outside request workspace was accepted")
	}
	if _, err := os.Stat(outsideFile); !os.IsNotExist(err) {
		t.Fatalf("outside file was created: %v", err)
	}
}

func TestFileWriteToolEditMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("alpha beta alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write := NewFileWriteTool(dir).Handler
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"ambiguous match", map[string]any{"path": "a.txt", "old_content": "alpha", "new_content": "x"}},
		{"missing match", map[string]any{"path": "a.txt", "old_content": "gamma", "new_content": "x"}},
		{"empty old_content", map[string]any{"path": "a.txt", "old_content": "", "new_content": "x"}},
		{"missing new_content", map[string]any{"path": "a.txt", "old_content": "beta"}},
		{"missing file", map[string]any{"path": "nope.txt", "old_content": "a", "new_content": "b"}},
		{"neither content nor edit", map[string]any{"path": "a.txt"}},
	} {
		if _, err := write(ctx, tc.args); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
	if data, _ := os.ReadFile(path); string(data) != "alpha beta alpha\n" {
		t.Fatalf("failed edits changed the file: %q", data)
	}

	if _, err := write(ctx, map[string]any{"path": "a.txt", "old_content": "beta", "new_content": "BETA"}); err != nil {
		t.Fatal(err)
	}
	if _, err := write(ctx, map[string]any{"path": "a.txt", "old_content": "alpha", "new_content": "A", "replace_all": true}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "A BETA A\n" {
		t.Fatalf("edited content = %q", data)
	}
}

// Models that fill every schema field send empty edit fields next to content;
// that must be treated as a whole-file write.
func TestFileWriteToolContentWithEmptyEditFields(t *testing.T) {
	dir := t.TempDir()
	write := NewFileWriteTool(dir).Handler
	args := map[string]any{"path": "docs/p.json", "content": "{}\n", "old_content": "", "new_content": "", "replace_all": false}
	if IsFileEdit(args) {
		t.Fatal("empty old_content with content must not select edit mode")
	}
	if _, err := write(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "docs", "p.json")); string(data) != "{}\n" {
		t.Fatalf("written content = %q", data)
	}
}
