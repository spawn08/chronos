package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
)

func TestInteractiveApprovalAllForSession(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "approval-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := input.WriteString("a\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = input
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = input.Close()
	})

	a := &agent.Agent{Tools: tool.NewRegistry()}
	for _, name := range []string{"first", "second"} {
		toolName := name
		a.Tools.Register(&tool.Definition{
			Name:       toolName,
			Permission: tool.PermRequireApproval,
			Handler: func(context.Context, map[string]any) (any, error) {
				return toolName, nil
			},
		})
	}
	installInteractiveApprovalHandlers(a)

	if _, err := a.Tools.Execute(context.Background(), "first", nil); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if a.Tools.PermissionMode() != tool.PermissionModeAutoApprove {
		t.Fatalf("permission mode = %q, want auto_approve", a.Tools.PermissionMode())
	}
	if _, err := a.Tools.Execute(context.Background(), "second", nil); err != nil {
		t.Fatalf("second Execute should not prompt: %v", err)
	}
}

func TestStripGlobalPermissionFlags(t *testing.T) {
	oldArgs := os.Args
	oldMode, hadMode := os.LookupEnv("CHRONOS_PERMISSION_MODE")
	oldDebug, hadDebug := os.LookupEnv("CHRONOS_DEBUG")
	oldTrace, hadTrace := os.LookupEnv("CHRONOS_TRACE")
	t.Cleanup(func() {
		os.Args = oldArgs
		restoreEnv("CHRONOS_PERMISSION_MODE", oldMode, hadMode)
		restoreEnv("CHRONOS_DEBUG", oldDebug, hadDebug)
		restoreEnv("CHRONOS_TRACE", oldTrace, hadTrace)
	})

	os.Args = []string{"chronos", "run", "--dangerously-skip-permissions", "--debug", "--trace", "hello"}
	if err := stripGlobalFlags(); err != nil {
		t.Fatalf("stripGlobalFlags: %v", err)
	}
	if got := os.Getenv("CHRONOS_PERMISSION_MODE"); got != string(tool.PermissionModeAutoApprove) {
		t.Fatalf("CHRONOS_PERMISSION_MODE = %q", got)
	}
	if os.Getenv("CHRONOS_DEBUG") != "true" || os.Getenv("CHRONOS_TRACE") != "true" {
		t.Fatal("debug/trace flags were not exported")
	}
	if len(os.Args) != 3 || os.Args[1] != "run" || os.Args[2] != "hello" {
		t.Fatalf("remaining args = %#v", os.Args)
	}
}

func TestStripGlobalNegativeRuntimeFlags(t *testing.T) {
	oldArgs := os.Args
	oldDebug, hadDebug := os.LookupEnv("CHRONOS_DEBUG")
	oldTrace, hadTrace := os.LookupEnv("CHRONOS_TRACE")
	t.Cleanup(func() {
		os.Args = oldArgs
		restoreEnv("CHRONOS_DEBUG", oldDebug, hadDebug)
		restoreEnv("CHRONOS_TRACE", oldTrace, hadTrace)
	})

	os.Args = []string{"chronos", "--no-debug", "--no-trace", "run", "hello"}
	if err := stripGlobalFlags(); err != nil {
		t.Fatalf("stripGlobalFlags: %v", err)
	}
	if os.Getenv("CHRONOS_DEBUG") != "false" || os.Getenv("CHRONOS_TRACE") != "false" {
		t.Fatalf("negative flags were not exported: debug=%q trace=%q", os.Getenv("CHRONOS_DEBUG"), os.Getenv("CHRONOS_TRACE"))
	}
}

// TestStripGlobalStreamFlagsPositionIndependent proves --stream/-s/--no-stream
// work regardless of where they appear on the command line — before the
// subcommand, after it, or after other arguments — matching the position
// independence stripGlobalFlags already gives --debug/--trace/--permission-mode.
// Before this fix, --stream/-s/--no-stream were parsed locally inside
// runAgent/teamRun's own arg loops, so `chronos --stream run <msg>` failed
// with "unknown command: --stream" since it never reached that loop.
func TestStripGlobalStreamFlagsPositionIndependent(t *testing.T) {
	oldArgs := os.Args
	oldStream, hadStream := os.LookupEnv("CHRONOS_STREAM")
	t.Cleanup(func() {
		os.Args = oldArgs
		restoreEnv("CHRONOS_STREAM", oldStream, hadStream)
	})

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "--stream before the subcommand", args: []string{"chronos", "--stream", "run", "hello"}, want: "true"},
		{name: "-s before the subcommand", args: []string{"chronos", "-s", "run", "hello"}, want: "true"},
		{name: "--stream after the subcommand", args: []string{"chronos", "run", "hello", "--stream"}, want: "true"},
		{name: "--no-stream before the subcommand", args: []string{"chronos", "--no-stream", "run", "hello"}, want: "false"},
		{name: "--no-stream after the subcommand", args: []string{"chronos", "team", "run", "myteam", "hello", "--no-stream"}, want: "false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Unsetenv("CHRONOS_STREAM")
			os.Args = append([]string(nil), tt.args...)
			if err := stripGlobalFlags(); err != nil {
				t.Fatalf("stripGlobalFlags(%v): %v", tt.args, err)
			}
			if got := os.Getenv("CHRONOS_STREAM"); got != tt.want {
				t.Fatalf("CHRONOS_STREAM = %q, want %q", got, tt.want)
			}
			for _, arg := range os.Args {
				if arg == "--stream" || arg == "-s" || arg == "--no-stream" {
					t.Fatalf("stream flag %q was not stripped from os.Args: %#v", arg, os.Args)
				}
			}
		})
	}
}

func TestApplyCLIRuntimeOverridesRejectsTracingWithoutStorage(t *testing.T) {
	t.Setenv("CHRONOS_TRACE", "true")
	a := &agent.Agent{ID: "no-store"}
	if err := applyCLIRuntimeOverrides(a); err == nil || !strings.Contains(err.Error(), "requires persistent storage") {
		t.Fatalf("applyCLIRuntimeOverrides error = %v, want persistent storage error", err)
	}
}

func TestStripGlobalFlagsRejectsInvalidValues(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	for _, args := range [][]string{
		{"chronos", "--permission-mode"},
		{"chronos", "--permission-mode=unknown", "repl"},
		{"chronos", "--config="},
		{"chronos", "-c"},
	} {
		os.Args = args
		if err := stripGlobalFlags(); err == nil {
			t.Errorf("stripGlobalFlags(%v) succeeded, want error", args)
		}
	}
}

func TestConfigValidateCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.yaml")
	if err := os.WriteFile(path, []byte("agents:\n  - id: dev\n    name: Dev\n    model:\n      provider: openai\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	oldArgs := os.Args
	oldConfig, hadConfig := os.LookupEnv("CHRONOS_CONFIG")
	os.Args = []string{"chronos", "config", "validate"}
	_ = os.Setenv("CHRONOS_CONFIG", path)
	t.Cleanup(func() {
		os.Args = oldArgs
		restoreEnv("CHRONOS_CONFIG", oldConfig, hadConfig)
	})
	out := captureStdout(t, func() {
		if err := runConfig(); err != nil {
			t.Fatalf("runConfig: %v", err)
		}
	})
	if !strings.Contains(out, "Configuration is valid: 1 agent(s), 0 team(s)") {
		t.Fatalf("output = %q", out)
	}
}

func restoreEnv(key, value string, existed bool) {
	if existed {
		_ = os.Setenv(key, value)
	} else {
		_ = os.Unsetenv(key)
	}
}
