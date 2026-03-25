package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSplitPrometheusLine_Squeeze(t *testing.T) {
	tests := []struct {
		line       string
		wantName   string
		wantVal    string
		wantOK     bool
	}{
		{`chronos_tool_calls_total 42`, "chronos_tool_calls_total", "42", true},
		{`chronos_model_latency_seconds_sum{le="0.5"} 1.25`, "chronos_model_latency_seconds_sum", "1.25", true},
		{`bare_metric 3.14`, "bare_metric", "3.14", true},
		{`onlyname`, "", "", false},
		{``, "", "", false},
	}
	for _, tt := range tests {
		n, v, ok := splitPrometheusLine(tt.line)
		if ok != tt.wantOK {
			t.Fatalf("line %q: ok=%v want %v", tt.line, ok, tt.wantOK)
		}
		if !tt.wantOK {
			continue
		}
		if n != tt.wantName || v != tt.wantVal {
			t.Errorf("line %q: got (%q,%q) want (%q,%q)", tt.line, n, v, tt.wantName, tt.wantVal)
		}
	}
}

func TestParsePrometheusText_Squeeze(t *testing.T) {
	text := `
# HELP x
chronos_tool_calls_total 2
chronos_tokens_used_total 10
chronos_model_calls_total 5
chronos_errors_total 1
chronos_active_sessions 3
chronos_model_latency_seconds_sum{le="1"} 0.5
not_a_number abc
`
	var st monitorStats
	parsePrometheusText(text, &st)
	if st.ToolCallsTotal != 2 || st.TokensUsedTotal != 10 || st.ModelCallsTotal != 5 {
		t.Fatalf("totals: %+v", st)
	}
	if st.ErrorsTotal != 1 || st.ActiveSessionsG != 3 {
		t.Fatalf("errors/gauge: %+v", st)
	}
	if st.ModelLatencyP50 != 0.5 {
		t.Fatalf("latency sum: %v", st.ModelLatencyP50)
	}
}

func TestFetchStats_Squeeze(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	now := time.Date(2024, 1, 2, 15, 4, 5, 0, time.UTC)
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, _ *http.Request) {
		type row struct {
			ID        string    `json:"id"`
			AgentID   string    `json:"agent_id"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"created_at"`
		}
		resp := struct {
			Sessions []row `json:"sessions"`
		}{
			Sessions: []row{
				{ID: "s-active", AgentID: "a1", Status: "active", CreatedAt: now},
				{ID: "s-err", AgentID: "a2", Status: "error", CreatedAt: now},
				{ID: "s-pend", AgentID: "a3", Status: "pending", CreatedAt: now},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("chronos_model_calls_total 4\nchronos_errors_total 1\nchronos_model_latency_seconds_sum 2\n"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := &http.Client{Timeout: 5 * time.Second}
	st := fetchStats(ctx, client, strings.TrimSuffix(srv.URL, "/"))
	if st.HealthStatus != "ok" {
		t.Fatalf("health: %q", st.HealthStatus)
	}
	if st.ActiveSessions != 1 || st.TotalSessions != 3 {
		t.Fatalf("sessions: active=%d total=%d", st.ActiveSessions, st.TotalSessions)
	}
	if st.ModelCallsTotal != 4 || st.ErrorsTotal != 1 {
		t.Fatalf("metrics: %+v", st)
	}
}

func TestRenderDashboard_Squeeze(t *testing.T) {
	longID := strings.Repeat("x", 30)
	st := monitorStats{
		HealthStatus:     "down",
		FetchErr:         "health: connection refused",
		RecentSessions:   nil,
		ToolCallsTotal:   1,
		ModelCallsTotal:  10,
		TokensUsedTotal:  100,
		ErrorsTotal:      5,
		ModelLatencyP50:  2.5,
		ActiveSessionsG:  7,
	}
	out := captureStdout(t, func() {
		renderDashboard(st, "http://test:8420", time.Second)
	})
	if !strings.Contains(out, "down") || !strings.Contains(out, "connection refused") {
		t.Fatalf("expected error health output, got: %q", out[:min(200, len(out))])
	}

	st2 := monitorStats{
		HealthStatus:    "ok",
		TotalSessions:   7,
		ModelCallsTotal: 100,
		ErrorsTotal:     15,
		ModelLatencyP50: 50,
	}
	st2.RecentSessions = append(st2.RecentSessions,
		sessionSummary{ID: longID, AgentID: strings.Repeat("y", 20), Status: "failed", CreatedAt: "12:00:00"},
		sessionSummary{ID: "s2", AgentID: "ag", Status: "paused", CreatedAt: "12:01:00"},
	)
	for i := 0; i < 5; i++ {
		st2.RecentSessions = append(st2.RecentSessions, sessionSummary{
			ID: fmt.Sprintf("id-%d", i), AgentID: "a", Status: "ok", CreatedAt: "t",
		})
	}
	_ = captureStdout(t, func() {
		renderDashboard(st2, "http://x", 2*time.Second)
	})

	st3 := monitorStats{HealthStatus: "ready", ModelCallsTotal: 0}
	_ = captureStdout(t, func() {
		renderDashboard(st3, "http://x", time.Second)
	})
}
