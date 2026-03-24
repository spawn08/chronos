package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/spawn08/chronos/sdk/agent"
	"github.com/spawn08/chronos/sdk/team"
)

func TestParseStrategy_AllBranches_Push(t *testing.T) {
	tests := []struct {
		in   string
		want team.Strategy
		ok   bool
	}{
		{"sequential", team.StrategySequential, true},
		{"SEQUENTIAL", team.StrategySequential, true},
		{"parallel", team.StrategyParallel, true},
		{"router", team.StrategyRouter, true},
		{"coordinator", team.StrategyCoordinator, true},
		{"unknown-mode", "", false},
	}
	for _, tt := range tests {
		got, err := parseStrategy(tt.in)
		if tt.ok {
			if err != nil || got != tt.want {
				t.Errorf("parseStrategy(%q) = %q, %v; want %q, nil", tt.in, got, err, tt.want)
			}
		} else {
			if err == nil || !strings.Contains(err.Error(), "unknown strategy") {
				t.Errorf("parseStrategy(%q) = %v, %v; want error", tt.in, got, err)
			}
		}
	}
}

func TestParseErrorStrategy_AllBranches_Push(t *testing.T) {
	tests := []struct {
		in   string
		want team.ErrorStrategy
		ok   bool
	}{
		{"fail_fast", team.ErrorStrategyFailFast, true},
		{"failfast", team.ErrorStrategyFailFast, true},
		{"collect", team.ErrorStrategyCollect, true},
		{"best_effort", team.ErrorStrategyBestEffort, true},
		{"besteffort", team.ErrorStrategyBestEffort, true},
		{"invalid", 0, false},
	}
	for _, tt := range tests {
		got, err := parseErrorStrategy(tt.in)
		if tt.ok {
			if err != nil || got != tt.want {
				t.Errorf("parseErrorStrategy(%q) = %v, %v; want %v, nil", tt.in, got, err, tt.want)
			}
		} else {
			if err == nil || !strings.Contains(err.Error(), "unknown error strategy") {
				t.Errorf("parseErrorStrategy(%q) = %v, %v; want error", tt.in, got, err)
			}
		}
	}
}

func TestStorageLabel_Push(t *testing.T) {
	if got := storageLabel(agent.StorageConfig{}); got != "sqlite (default)" {
		t.Fatalf("empty config: got %q", got)
	}
	longDSN := strings.Repeat("z", 50)
	got := storageLabel(agent.StorageConfig{Backend: "postgres", DSN: longDSN})
	if !strings.Contains(got, "...") {
		t.Fatalf("expected truncated DSN in %q", got)
	}
	short := storageLabel(agent.StorageConfig{Backend: "mem"})
	if short != "mem" {
		t.Fatalf("got %q", short)
	}
}

func TestHumanizeBytes_Push(t *testing.T) {
	if humanizeBytes(0) != "0 B" {
		t.Fatalf("0: %q", humanizeBytes(0))
	}
	if humanizeBytes(500) != "500 B" {
		t.Fatalf("500: %q", humanizeBytes(500))
	}
	kb := humanizeBytes(2048)
	if !strings.Contains(kb, "KB") {
		t.Fatalf("2048: %q", kb)
	}
}

func TestMaskEnv_Push(t *testing.T) {
	t.Setenv("CHRONOS_PUSH_MASK_TEST", "")
	if got := maskEnv("CHRONOS_PUSH_MASK_TEST"); got != "(not set)" {
		t.Fatalf("empty: %q", got)
	}
	t.Setenv("CHRONOS_PUSH_MASK_TEST", "short")
	if got := maskEnv("CHRONOS_PUSH_MASK_TEST"); got != "****" {
		t.Fatalf("short: %q", got)
	}
	t.Setenv("CHRONOS_PUSH_MASK_TEST", "abcdefghijklmnop")
	if got := maskEnv("CHRONOS_PUSH_MASK_TEST"); got != "abcd...mnop" {
		t.Fatalf("long: %q", got)
	}
}

func TestEnvOrDefault_Push(t *testing.T) {
	key := "CHRONOS_PUSH_ENV_OR_DEFAULT_XYZ"
	_ = os.Unsetenv(key)
	if got := envOrDefault(key, "fallback"); got != "fallback" {
		t.Fatalf("unset: %q", got)
	}
	t.Setenv(key, "from-env")
	if got := envOrDefault(key, "fallback"); got != "from-env" {
		t.Fatalf("set: %q", got)
	}
}
