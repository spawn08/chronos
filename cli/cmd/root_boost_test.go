package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestExecute_Version_Boost(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "version"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "chronos") {
		t.Errorf("expected version output, got %q", out[:min(120, len(out))])
	}
}

func TestExecute_HelpAliases_Boost(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		arg := arg
		t.Run(arg, func(t *testing.T) {
			old := os.Args
			t.Cleanup(func() { os.Args = old })
			os.Args = []string{"chronos", arg}

			out := captureStdout(t, func() {
				if err := Execute(); err != nil {
					t.Fatalf("Execute: %v", err)
				}
			})
			if !strings.Contains(out, "Chronos CLI") {
				t.Errorf("expected usage banner for %q", arg)
			}
		})
	}
}

func TestExecute_NoArgs_Boost(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "Usage:") {
		t.Errorf("expected usage when no subcommand, got %q", out[:min(200, len(out))])
	}
}

func TestExecute_UnknownCommand_Boost(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "not-a-real-command-xyz"}

	err := Execute()
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("error = %v", err)
	}
}

func TestExecute_MonitorHelp_Boost(t *testing.T) {
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = []string{"chronos", "monitor", "--help"}

	out := captureStdout(t, func() {
		if err := Execute(); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	})
	if !strings.Contains(out, "chronos monitor") || !strings.Contains(out, "--endpoint") {
		t.Errorf("expected monitor help text, got %q", out[:min(300, len(out))])
	}
}
