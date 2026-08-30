package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestRunAuthCmd(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	t.Run("usage error with no subcommand", func(t *testing.T) {
		os.Args = []string{"chronos", "auth"}
		if err := runAuthCmd(); err == nil {
			t.Fatal("expected a usage error")
		}
	})

	t.Run("errors when CHRONOS_AUTH is unset", func(t *testing.T) {
		t.Setenv(envAuthMode, "")
		os.Args = []string{"chronos", "auth", "token"}
		if err := runAuthCmd(); err == nil {
			t.Fatal("expected an error when no auth mode is configured")
		}
	})

	t.Run("apikey mode prints a usable CHRONOS_API_KEYS entry", func(t *testing.T) {
		t.Setenv(envAuthMode, "apikey")
		os.Args = []string{"chronos", "auth", "token", "--role", "viewer", "--tenant", "acme"}
		out := captureStdout(t, func() {
			if err := runAuthCmd(); err != nil {
				t.Errorf("runAuthCmd: %v", err)
			}
		})
		if !strings.Contains(out, envAPIKeys+"=") {
			t.Errorf("output missing %s=: %q", envAPIKeys, out)
		}
		if !strings.Contains(out, ":viewer:acme") {
			t.Errorf("output missing role/tenant suffix: %q", out)
		}
	})

	t.Run("jwt mode requires a secret", func(t *testing.T) {
		t.Setenv(envAuthMode, "jwt")
		t.Setenv(envJWTSecret, "")
		os.Args = []string{"chronos", "auth", "token"}
		if err := runAuthCmd(); err == nil {
			t.Fatal("expected an error when CHRONOS_JWT_SECRET is unset")
		}
	})

	t.Run("jwt mode prints a signed token", func(t *testing.T) {
		t.Setenv(envAuthMode, "jwt")
		t.Setenv(envJWTSecret, "s3cr3t")
		os.Args = []string{"chronos", "auth", "token", "--ttl", "1h"}
		out := captureStdout(t, func() {
			if err := runAuthCmd(); err != nil {
				t.Errorf("runAuthCmd: %v", err)
			}
		})
		if !strings.Contains(out, "Authorization: Bearer ") {
			t.Errorf("output missing bearer usage example: %q", out)
		}
		parts := strings.Split(strings.TrimSpace(out), "\n")
		found := false
		for _, line := range parts {
			if strings.Count(strings.TrimSpace(line), ".") == 2 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a 3-segment JWT in the output: %q", out)
		}
	})

	t.Run("unknown flag is rejected", func(t *testing.T) {
		t.Setenv(envAuthMode, "apikey")
		os.Args = []string{"chronos", "auth", "token", "--bogus"}
		if err := runAuthCmd(); err == nil {
			t.Fatal("expected an error for an unknown flag")
		}
	})
}
