package cmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSplitPrometheusLine_NoLabels(t *testing.T) {
	tests := []struct {
		line      string
		wantName  string
		wantValue string
		wantOK    bool
	}{
		{"chronos_tool_calls_total 42", "chronos_tool_calls_total", "42", true},
		{"metric_name 3.14", "metric_name", "3.14", true},
		{"only_one_token", "", "", false},
		{"", "", "", false},
		{"name value timestamp", "name", "value", true},
	}
	for _, tt := range tests {
		name, val, ok := splitPrometheusLine(tt.line)
		if ok != tt.wantOK {
			t.Errorf("splitPrometheusLine(%q) ok=%v, want %v", tt.line, ok, tt.wantOK)
			continue
		}
		if ok {
			if name != tt.wantName {
				t.Errorf("splitPrometheusLine(%q) name=%q, want %q", tt.line, name, tt.wantName)
			}
			if val != tt.wantValue {
				t.Errorf("splitPrometheusLine(%q) value=%q, want %q", tt.line, val, tt.wantValue)
			}
		}
	}
}

func TestSplitPrometheusLine_WithLabels(t *testing.T) {
	line := `chronos_model_calls_total{provider="openai",model="gpt-4o"} 100`
	name, val, ok := splitPrometheusLine(line)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "chronos_model_calls_total" {
		t.Errorf("name=%q, want chronos_model_calls_total", name)
	}
	if val != "100" {
		t.Errorf("val=%q, want 100", val)
	}
}

func TestParsePrometheusText(t *testing.T) {
	text := `# HELP chronos_tool_calls_total Total tool calls
# TYPE chronos_tool_calls_total counter
chronos_tool_calls_total 15
chronos_tokens_used_total 2048
chronos_model_calls_total 10
chronos_errors_total 2
chronos_active_sessions 3
chronos_model_latency_seconds_sum 5.5
`
	var stats monitorStats
	parsePrometheusText(text, &stats)

	if stats.ToolCallsTotal != 15 {
		t.Errorf("ToolCallsTotal=%v, want 15", stats.ToolCallsTotal)
	}
	if stats.TokensUsedTotal != 2048 {
		t.Errorf("TokensUsedTotal=%v, want 2048", stats.TokensUsedTotal)
	}
	if stats.ModelCallsTotal != 10 {
		t.Errorf("ModelCallsTotal=%v, want 10", stats.ModelCallsTotal)
	}
	if stats.ErrorsTotal != 2 {
		t.Errorf("ErrorsTotal=%v, want 2", stats.ErrorsTotal)
	}
	if stats.ActiveSessionsG != 3 {
		t.Errorf("ActiveSessionsG=%v, want 3", stats.ActiveSessionsG)
	}
	if stats.ModelLatencyP50 != 5.5 {
		t.Errorf("ModelLatencyP50=%v, want 5.5", stats.ModelLatencyP50)
	}
}

func TestParsePrometheusText_Empty(t *testing.T) {
	var stats monitorStats
	parsePrometheusText("", &stats)
	if stats.ToolCallsTotal != 0 {
		t.Errorf("expected 0, got %v", stats.ToolCallsTotal)
	}
}

func TestFetchStats_HealthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/api/sessions":
			w.Write([]byte(`{"sessions":[]}`))
		case "/metrics":
			w.Write([]byte(""))
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	stats := fetchStats(t.Context(), client, srv.URL)

	if stats.HealthStatus != "ok" {
		t.Errorf("HealthStatus=%q, want ok", stats.HealthStatus)
	}
}

func TestFetchStats_HealthUnreachable(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	stats := fetchStats(t.Context(), client, "http://127.0.0.1:19999")

	if stats.HealthStatus != "unreachable" {
		t.Errorf("HealthStatus=%q, want unreachable", stats.HealthStatus)
	}
	if stats.FetchErr == "" {
		t.Error("expected FetchErr to be set")
	}
}

func TestFetchStats_WithSessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/api/sessions":
			w.Write([]byte(`{"sessions":[
				{"id":"s1","agent_id":"agent1","status":"running","created_at":"2026-03-25T10:00:00Z"},
				{"id":"s2","agent_id":"agent2","status":"completed","created_at":"2026-03-25T10:01:00Z"}
			]}`))
		case "/metrics":
			w.Write([]byte("chronos_tool_calls_total 5\n"))
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	stats := fetchStats(t.Context(), client, srv.URL)

	if stats.TotalSessions != 2 {
		t.Errorf("TotalSessions=%d, want 2", stats.TotalSessions)
	}
	if stats.ActiveSessions != 1 {
		t.Errorf("ActiveSessions=%d, want 1 (running)", stats.ActiveSessions)
	}
	if stats.ToolCallsTotal != 5 {
		t.Errorf("ToolCallsTotal=%v, want 5", stats.ToolCallsTotal)
	}
}

func TestRenderDashboard_NoError(t *testing.T) {
	stats := monitorStats{
		HealthStatus:    "ok",
		ActiveSessions:  2,
		TotalSessions:   5,
		ToolCallsTotal:  10,
		ModelCallsTotal: 8,
		TokensUsedTotal: 1500,
		ErrorsTotal:     1,
		ModelLatencyP50: 2.0,
		FetchedAt:       time.Now(),
	}
	// Just ensure no panic
	output := captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 2*time.Second)
	})
	if !strings.Contains(output, "CHRONOS MONITOR") {
		t.Errorf("expected CHRONOS MONITOR in output, got: %q", output[:min(200, len(output))])
	}
}

func TestRenderDashboard_WithSessions(t *testing.T) {
	stats := monitorStats{
		HealthStatus: "unreachable",
		FetchErr:     "connection refused",
		RecentSessions: []sessionSummary{
			{ID: "s1", AgentID: "a1", Status: "running", CreatedAt: "10:00:00"},
			{ID: "s2", AgentID: "a2", Status: "error", CreatedAt: "10:01:00"},
			{ID: "s3", AgentID: "a3", Status: "paused", CreatedAt: "10:02:00"},
		},
		FetchedAt: time.Now(),
	}
	// Ensure no panic with sessions
	captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 5*time.Second)
	})
}

func TestRenderDashboard_ManySessionsTruncated(t *testing.T) {
	sessions := make([]sessionSummary, 10)
	for i := range sessions {
		sessions[i] = sessionSummary{ID: "sess-with-very-long-id-1234567890", AgentID: "very-long-agent-id", Status: "running"}
	}
	stats := monitorStats{
		HealthStatus:   "ok",
		RecentSessions: sessions,
		FetchedAt:      time.Now(),
	}
	output := captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 2*time.Second)
	})
	if !strings.Contains(output, "more") {
		t.Errorf("expected truncation note for >5 sessions, got: %q", output[:min(500, len(output))])
	}
}

func TestRenderDashboard_NoSessions(t *testing.T) {
	stats := monitorStats{
		HealthStatus: "ok",
		FetchedAt:    time.Now(),
	}
	output := captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 2*time.Second)
	})
	if !strings.Contains(output, "No sessions found") {
		t.Errorf("expected 'No sessions found', got: %q", output[:min(500, len(output))])
	}
}

func TestRenderDashboard_ErrorRate(t *testing.T) {
	stats := monitorStats{
		HealthStatus:    "ok",
		ModelCallsTotal: 100,
		ErrorsTotal:     50, // 50% error rate → red
		FetchedAt:       time.Now(),
	}
	// Just ensure no panic
	captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 2*time.Second)
	})
}

func TestRenderDashboard_ActiveSessionsGauge(t *testing.T) {
	stats := monitorStats{
		HealthStatus:    "ok",
		ActiveSessions:  1,
		ActiveSessionsG: 5, // gauge takes precedence
		FetchedAt:       time.Now(),
	}
	output := captureStdout(t, func() {
		renderDashboard(stats, "http://localhost:8420", 2*time.Second)
	})
	if !strings.Contains(output, "5") {
		t.Errorf("expected gauge value 5 in output")
	}
}

func TestHTTPGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	body, err := httpGet(t.Context(), client, srv.URL)
	if err != nil {
		t.Fatalf("httpGet: %v", err)
	}
	if !strings.Contains(string(body), "ok") {
		t.Errorf("unexpected body: %q", body)
	}
}

func TestHTTPGet_Failure(t *testing.T) {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	_, err := httpGet(t.Context(), client, "http://127.0.0.1:19999")
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
