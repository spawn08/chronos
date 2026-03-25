package repl

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/storage/adapters/memory"
)

func TestStart_SessionsCommandEmpty_Squeeze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdin pipe")
	}
	st := memory.New()
	r := New(st)

	oldIn := os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = pr.Close()
	})
	go func() {
		_, _ = pw.WriteString("/sessions\n/quit\n")
		_ = pw.Close()
	}()

	_ = captureStdout(t, func() {
		if err := r.Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})
}

func TestStart_WithAgent_ModelSlashCommand_Squeeze(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stdin pipe")
	}
	store := newTestStore(t)
	r := New(store)
	a, err := agent.New("aid", "AgentName").Build()
	if err != nil {
		t.Fatal(err)
	}
	r.SetAgent(a)

	oldIn := os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr
	t.Cleanup(func() {
		os.Stdin = oldIn
		_ = pr.Close()
	})
	go func() {
		_, _ = pw.WriteString("/model\n/agent\n/quit\n")
		_ = pw.Close()
	}()

	out := captureStdout(t, func() {
		if err := r.Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})
	if !strings.Contains(out, "No model configured") && !strings.Contains(out, "AgentName") {
		t.Fatalf("unexpected output: %q", out)
	}
}
