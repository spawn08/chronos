package cmd

import (
	"os"
	"strings"
	"testing"
)

// withArgs sets os.Args for the duration of the test and restores it after.
func withArgs(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	t.Cleanup(func() { os.Args = old })
	os.Args = append([]string{"chronos"}, args...)
}

// TestRunAgent_TrailingAgentFlagErrors verifies a trailing --agent with no value
// is reported as an error rather than silently sent as the chat message.
func TestRunAgent_TrailingAgentFlagErrors(t *testing.T) {
	for _, flag := range []string{"--agent", "-a"} {
		withArgs(t, "run", flag)
		err := Execute()
		if err == nil {
			t.Fatalf("%s with no value: expected error, got nil", flag)
		}
		if !strings.Contains(err.Error(), "requires an agent id") {
			t.Fatalf("%s: unexpected error: %v", flag, err)
		}
	}
}

// TestRunMonitor_InvalidInterval verifies bad --interval values are rejected.
func TestRunMonitor_InvalidInterval(t *testing.T) {
	for _, val := range []string{"abc", "0", "-3"} {
		withArgs(t, "monitor", "--interval", val)
		err := Execute()
		if err == nil {
			t.Fatalf("--interval %q: expected error, got nil", val)
		}
		if !strings.Contains(err.Error(), "invalid --interval") {
			t.Fatalf("--interval %q: unexpected error: %v", val, err)
		}
	}
}

// TestRunMonitor_UnknownFlag verifies an unrecognized flag is rejected instead
// of silently running with defaults.
func TestRunMonitor_UnknownFlag(t *testing.T) {
	withArgs(t, "monitor", "--intrval", "5") // typo
	err := Execute()
	if err == nil {
		t.Fatal("unknown flag: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown monitor flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestParsePrometheus_LatencyCount verifies the histogram sum AND count are both
// parsed so average latency uses the histogram's own denominator.
func TestParsePrometheus_LatencyCount(t *testing.T) {
	text := `chronos_model_calls_total 8
chronos_model_latency_seconds_sum 4.0
chronos_model_latency_seconds_count 10`
	var stats monitorStats
	parsePrometheusText(text, &stats)

	if stats.ModelLatencySum != 4.0 {
		t.Errorf("ModelLatencySum = %v, want 4.0", stats.ModelLatencySum)
	}
	if stats.ModelLatencyCount != 10 {
		t.Errorf("ModelLatencyCount = %v, want 10", stats.ModelLatencyCount)
	}
	// Average latency is Sum/Count = 0.4s = 400ms, independent of ModelCallsTotal.
	avgMs := (stats.ModelLatencySum / stats.ModelLatencyCount) * 1000
	if avgMs != 400 {
		t.Errorf("avg latency = %v ms, want 400", avgMs)
	}
}
