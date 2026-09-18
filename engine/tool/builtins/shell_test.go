package builtins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// TestShellTool_Permissions asserts that the host shell tools never
// auto-approve on the host, while the sandbox-backed shell may.
func TestShellTool_Permissions(t *testing.T) {
	tests := []struct {
		name string
		def  *tool.Definition
		want tool.Permission
	}{
		{"NewShellTool requires approval", NewShellTool(nil, 0), tool.PermRequireApproval},
		{"NewAutoShellTool requires approval (host, no isolation)", NewAutoShellTool(nil, 0), tool.PermRequireApproval},
		{"NewSandboxShellTool auto-allowed (confined)", NewSandboxShellTool(nil, 0), tool.PermAllow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.def.Permission != tc.want {
				t.Errorf("Permission = %v, want %v", tc.def.Permission, tc.want)
			}
		})
	}
}

func TestShellToolUsesRequestWorkspaceAndContainedWorkingDirectory(t *testing.T) {
	configured := t.TempDir()
	requestRoot := t.TempDir()
	subdir := filepath.Join(requestRoot, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	sh := NewShellToolAt(configured, nil, 5*time.Second)
	result, err := sh.Handler(WithWorkspaceRoot(context.Background(), requestRoot), map[string]any{
		"command": "pwd", "working_dir": "subdir",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := subdir
	if canonical, err := filepath.EvalSymlinks(want); err == nil {
		want = canonical
	}
	if got := filepath.Clean(strings.TrimSpace(result.(map[string]any)["stdout"].(string))); got != want {
		t.Fatalf("pwd = %q, want %q", got, want)
	}
	if _, err := sh.Handler(WithWorkspaceRoot(context.Background(), requestRoot), map[string]any{
		"command": "pwd", "working_dir": filepath.Dir(requestRoot),
	}); err == nil {
		t.Fatal("outside working directory was accepted")
	}
}

func TestShellTool_Echo(t *testing.T) {
	sh := NewShellTool(nil, 5*time.Second)
	result, err := sh.Handler(context.Background(), map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["stdout"] != "hello\n" {
		t.Errorf("stdout = %q", m["stdout"])
	}
	if m["exit_code"] != 0 {
		t.Errorf("exit_code = %v", m["exit_code"])
	}
}

func TestShellTool_AllowedCommands(t *testing.T) {
	sh := NewShellTool([]string{"echo"}, 5*time.Second)

	_, err := sh.Handler(context.Background(), map[string]any{
		"command": "echo ok",
	})
	if err != nil {
		t.Fatalf("allowed command should succeed: %v", err)
	}

	_, err = sh.Handler(context.Background(), map[string]any{
		"command": "rm -rf /",
	})
	if err == nil {
		t.Fatal("disallowed command should fail")
		return
	}
}

func TestShellTool_MissingCommand(t *testing.T) {
	sh := NewShellTool(nil, 5*time.Second)
	_, err := sh.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command")
		return
	}
}

func TestShellTool_NonZeroExit(t *testing.T) {
	sh := NewShellTool(nil, 5*time.Second)
	result, err := sh.Handler(context.Background(), map[string]any{
		"command": "exit 42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["exit_code"] != 42 {
		t.Errorf("exit_code = %v, want 42", m["exit_code"])
	}
}

func TestShellTool_Timeout(t *testing.T) {
	sh := NewShellTool(nil, 100*time.Millisecond)
	result, err := sh.Handler(context.Background(), map[string]any{
		"command": "sleep 10",
	})
	if err != nil {
		return
	}
	m := result.(map[string]any)
	if m["exit_code"] == 0 {
		t.Fatal("expected non-zero exit code on timeout")
	}
}
