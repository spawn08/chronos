// Package cmd provides the Chronos CLI command tree.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// monitorStats holds the aggregated data for the monitor dashboard.
type monitorStats struct {
	// From /health
	HealthStatus string `json:"health_status"`

	// From /api/sessions
	ActiveSessions int              `json:"active_sessions"`
	RecentSessions []sessionSummary `json:"recent_sessions"`
	TotalSessions  int              `json:"total_sessions"`

	// From /metrics (Prometheus text format parsed)
	ToolCallsTotal  float64 `json:"tool_calls_total"`
	TokensUsedTotal float64 `json:"tokens_used_total"`
	ModelCallsTotal float64 `json:"model_calls_total"`
	ErrorsTotal     float64 `json:"errors_total"`
	ModelLatencyP50 float64 `json:"model_latency_p50"`
	ActiveSessionsG float64 `json:"active_sessions_gauge"`

	FetchedAt time.Time `json:"fetched_at"`
	FetchErr  string    `json:"fetch_err,omitempty"`
}

// sessionSummary is a lightweight view of a session for the dashboard.
type sessionSummary struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// apiSessionsResponse mirrors the JSON from /api/sessions.
type apiSessionsResponse struct {
	Sessions []struct {
		ID        string    `json:"id"`
		AgentID   string    `json:"agent_id"`
		Status    string    `json:"status"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"sessions"`
}

// runMonitor is the entry point for `chronos monitor`.
func runMonitor() error {
	endpoint := "http://localhost:8420"
	interval := 2 * time.Second

	// Parse flags: --endpoint <url> --interval <seconds>
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--endpoint", "-e":
			if i+1 < len(args) {
				endpoint = args[i+1]
				i++
			}
		case "--interval", "-i":
			if i+1 < len(args) {
				if secs, err := strconv.Atoi(args[i+1]); err == nil && secs > 0 {
					interval = time.Duration(secs) * time.Second
				}
				i++
			}
		case "--help", "-h":
			fmt.Println(`Usage: chronos monitor [--endpoint <url>] [--interval <seconds>]

Options:
  --endpoint, -e   ChronosOS HTTP endpoint (default: http://localhost:8420)
  --interval, -i   Refresh interval in seconds (default: 2)

Displays a live terminal dashboard polling the ChronosOS control plane.
Press Ctrl+C to exit.`)
			return nil
		}
	}

	// Override endpoint from env if set.
	if v := os.Getenv("CHRONOS_ENDPOINT"); v != "" {
		endpoint = v
	}
	endpoint = strings.TrimRight(endpoint, "/")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C gracefully.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
	}()

	// Hide cursor.
	fmt.Print("\033[?25l")
	// Restore cursor on exit.
	defer fmt.Print("\033[?25h\033[0m\n")

	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Render immediately on start.
	stats := fetchStats(ctx, client, endpoint)
	renderDashboard(stats, endpoint, interval)

	for {
		select {
		case <-ctx.Done():
			clearScreen()
			fmt.Println("Monitor stopped.")
			return nil
		case <-ticker.C:
			stats = fetchStats(ctx, client, endpoint)
			renderDashboard(stats, endpoint, interval)
		}
	}
}

// fetchStats collects metrics from the ChronosOS HTTP API.
func fetchStats(ctx context.Context, client *http.Client, endpoint string) monitorStats {
	stats := monitorStats{
		FetchedAt:    time.Now(),
		HealthStatus: "unknown",
	}

	// --- /health ---
	if body, err := httpGet(ctx, client, endpoint+"/health"); err == nil {
		var h struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(body, &h) == nil && h.Status != "" {
			stats.HealthStatus = h.Status
		}
	} else {
		stats.HealthStatus = "unreachable"
		stats.FetchErr = fmt.Sprintf("health: %v", err)
	}

	// --- /api/sessions ---
	if body, err := httpGet(ctx, client, endpoint+"/api/sessions?limit=10"); err == nil {
		var resp apiSessionsResponse
		if json.Unmarshal(body, &resp) == nil {
			stats.TotalSessions = len(resp.Sessions)
			for _, s := range resp.Sessions {
				sum := sessionSummary{
					ID:        s.ID,
					AgentID:   s.AgentID,
					Status:    s.Status,
					CreatedAt: s.CreatedAt.Format("15:04:05"),
				}
				stats.RecentSessions = append(stats.RecentSessions, sum)
				if s.Status == "active" || s.Status == "running" {
					stats.ActiveSessions++
				}
			}
		}
	}

	// --- /metrics (Prometheus text) ---
	if body, err := httpGet(ctx, client, endpoint+"/metrics"); err == nil {
		parsePrometheusText(string(body), &stats)
	}

	return stats
}

// httpGet performs a GET request and returns the response body.
func httpGet(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http get %s: %w", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return data, nil
}

// parsePrometheusText extracts key metrics from the Prometheus text exposition format.
func parsePrometheusText(text string, stats *monitorStats) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, valStr, ok := splitPrometheusLine(line)
		if !ok {
			continue
		}
		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		switch {
		case name == "chronos_tool_calls_total":
			stats.ToolCallsTotal += val
		case name == "chronos_tokens_used_total":
			stats.TokensUsedTotal += val
		case name == "chronos_model_calls_total":
			stats.ModelCallsTotal += val
		case name == "chronos_errors_total":
			stats.ErrorsTotal += val
		case name == "chronos_active_sessions":
			stats.ActiveSessionsG = val
		case strings.HasPrefix(name, "chronos_model_latency_seconds_sum"):
			// Store sum for computing average latency below.
			stats.ModelLatencyP50 += val
		}
	}
}

// splitPrometheusLine extracts the metric name (without labels) and value.
func splitPrometheusLine(line string) (name, value string, ok bool) {
	// Format: metric_name{labels} value [timestamp]
	// or:     metric_name value
	var rest string
	if idx := strings.Index(line, "{"); idx != -1 {
		name = line[:idx]
		rest = line[strings.Index(line, "}")+1:]
	} else {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			return "", "", false
		}
		name = parts[0]
		rest = parts[1]
	}
	name = strings.TrimSpace(name)
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	return name, fields[0], true
}

// renderDashboard clears the screen and redraws the dashboard.
func renderDashboard(stats monitorStats, endpoint string, interval time.Duration) {
	clearScreen()

	// Title bar
	fmt.Printf("\033[1;36m╔══════════════════════════════════════════════════════════════╗\033[0m\n")
	fmt.Printf("\033[1;36m║           CHRONOS MONITOR  %-34s║\033[0m\n",
		fmt.Sprintf("%-34s", time.Now().Format("2006-01-02 15:04:05")))
	fmt.Printf("\033[1;36m╚══════════════════════════════════════════════════════════════╝\033[0m\n")
	fmt.Println()

	// Connection info
	statusColor := "\033[32m" // green
	if stats.HealthStatus != "ok" && stats.HealthStatus != "alive" && stats.HealthStatus != "ready" {
		statusColor = "\033[31m" // red
	}
	fmt.Printf("  Endpoint : \033[33m%s\033[0m\n", endpoint)
	fmt.Printf("  Health   : %s%s\033[0m\n", statusColor, stats.HealthStatus)
	fmt.Printf("  Refresh  : every %s\n", interval)
	if stats.FetchErr != "" {
		fmt.Printf("  \033[31mError    : %s\033[0m\n", stats.FetchErr)
	}
	fmt.Println()

	// Sessions panel
	fmt.Printf("\033[1;33m── Sessions ─────────────────────────────────────────────────────\033[0m\n")
	activeSessions := stats.ActiveSessions
	if stats.ActiveSessionsG > 0 {
		activeSessions = int(stats.ActiveSessionsG)
	}
	fmt.Printf("  Active   : \033[1;32m%d\033[0m\n", activeSessions)
	fmt.Printf("  Listed   : %d\n", stats.TotalSessions)
	fmt.Println()

	if len(stats.RecentSessions) > 0 {
		fmt.Printf("  \033[2m%-24s  %-12s  %-10s  %s\033[0m\n", "ID", "AGENT", "STATUS", "TIME")
		fmt.Printf("  %s\n", strings.Repeat("─", 62))
		shown := stats.RecentSessions
		if len(shown) > 5 {
			shown = shown[:5]
		}
		for _, s := range shown {
			id := s.ID
			if len(id) > 22 {
				id = id[:10] + "…" + id[len(id)-10:]
			}
			agent := s.AgentID
			if len(agent) > 12 {
				agent = agent[:11] + "…"
			}
			statusCol := "\033[32m"
			if s.Status == "error" || s.Status == "failed" {
				statusCol = "\033[31m"
			} else if s.Status == "paused" || s.Status == "pending" {
				statusCol = "\033[33m"
			}
			fmt.Printf("  %-24s  %-12s  %s%-10s\033[0m  %s\n",
				id, agent, statusCol, s.Status, s.CreatedAt)
		}
		if len(stats.RecentSessions) > 5 {
			fmt.Printf("  \033[2m… and %d more\033[0m\n", len(stats.RecentSessions)-5)
		}
	} else {
		fmt.Printf("  \033[2mNo sessions found.\033[0m\n")
	}
	fmt.Println()

	// Metrics panel
	fmt.Printf("\033[1;33m── Metrics ──────────────────────────────────────────────────────\033[0m\n")
	fmt.Printf("  Tool Calls   : \033[1m%.0f\033[0m\n", stats.ToolCallsTotal)
	fmt.Printf("  Model Calls  : \033[1m%.0f\033[0m\n", stats.ModelCallsTotal)
	fmt.Printf("  Tokens Used  : \033[1m%.0f\033[0m\n", stats.TokensUsedTotal)

	errColor := "\033[0m"
	if stats.ErrorsTotal > 0 {
		errColor = "\033[31m"
	}
	fmt.Printf("  Errors       : %s\033[1m%.0f\033[0m\n", errColor, stats.ErrorsTotal)

	// Compute error rate (errors / model calls).
	if stats.ModelCallsTotal > 0 {
		rate := (stats.ErrorsTotal / stats.ModelCallsTotal) * 100
		rateColor := "\033[32m"
		if rate > 10 {
			rateColor = "\033[31m"
		} else if rate > 2 {
			rateColor = "\033[33m"
		}
		fmt.Printf("  Error Rate   : %s%.1f%%\033[0m\n", rateColor, rate)
	} else {
		fmt.Printf("  Error Rate   : \033[2mn/a\033[0m\n")
	}

	// Avg latency from sum metric (approx).
	if stats.ModelCallsTotal > 0 && stats.ModelLatencyP50 > 0 {
		avgMs := (stats.ModelLatencyP50 / stats.ModelCallsTotal) * 1000
		fmt.Printf("  Avg Latency  : \033[1m%.0f ms\033[0m\n", avgMs)
	} else {
		fmt.Printf("  Avg Latency  : \033[2mn/a\033[0m\n")
	}
	fmt.Println()

	fmt.Printf("\033[2mPress Ctrl+C to exit.\033[0m\n")
}

// clearScreen moves the cursor to the top-left and clears the terminal.
func clearScreen() {
	fmt.Print("\033[H\033[2J")
}
