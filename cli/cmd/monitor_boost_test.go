package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePrometheusText_SkipsInvalidFloat_Boost(t *testing.T) {
	text := `chronos_tool_calls_total not-a-number
chronos_tool_calls_total 7
`
	var stats monitorStats
	parsePrometheusText(text, &stats)
	if stats.ToolCallsTotal != 7 {
		t.Errorf("ToolCallsTotal = %v, want 7", stats.ToolCallsTotal)
	}
}

func TestParsePrometheusText_LabeledMetricsAccumulate_Boost(t *testing.T) {
	text := `chronos_model_latency_seconds_sum{model="a"} 1.0
chronos_model_latency_seconds_sum{model="b"} 2.5
`
	var stats monitorStats
	parsePrometheusText(text, &stats)
	if stats.ModelLatencyP50 != 3.5 {
		t.Errorf("ModelLatencyP50 = %v, want 3.5", stats.ModelLatencyP50)
	}
}

func TestParsePrometheusText_WhitespaceAndComments_Boost(t *testing.T) {
	text := `
# comment line
   

chronos_errors_total  3  
`
	var stats monitorStats
	parsePrometheusText(text, &stats)
	if stats.ErrorsTotal != 3 {
		t.Errorf("ErrorsTotal = %v", stats.ErrorsTotal)
	}
}

func TestFetchStats_HealthJSONMissingStatus_Boost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{}`))
		case "/api/sessions":
			w.Write([]byte(`{"sessions":[]}`))
		case "/metrics":
			w.Write([]byte(""))
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	stats := fetchStats(context.Background(), client, srv.URL)
	if stats.HealthStatus != "unknown" {
		t.Errorf("HealthStatus = %q, want unknown when status field empty", stats.HealthStatus)
	}
}

func TestFetchStats_SessionsInvalidJSON_Boost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Write([]byte(`{"status":"ok"}`))
		case "/api/sessions":
			w.Write([]byte(`not-json`))
		case "/metrics":
			w.Write([]byte(""))
		}
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	stats := fetchStats(context.Background(), client, srv.URL)
	if stats.TotalSessions != 0 || len(stats.RecentSessions) != 0 {
		t.Errorf("expected no sessions on bad JSON, got total=%d recent=%d", stats.TotalSessions, len(stats.RecentSessions))
	}
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (errReadCloser) Close() error             { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPGet_ReadBodyError_Boost(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(errReadCloser{}),
			}, nil
		}),
	}
	_, err := httpGet(context.Background(), client, "http://example.invalid/any")
	if err == nil || !strings.Contains(err.Error(), "read body") {
		t.Fatalf("expected read body error, got %v", err)
	}
}
