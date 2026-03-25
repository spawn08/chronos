package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRegistry_PreRegistered(t *testing.T) {
	r := NewRegistry()
	if _, ok := r.counters["chronos_agent_runs_total"]; !ok {
		t.Error("missing chronos_agent_runs_total")
	}
	if _, ok := r.counters["chronos_tool_calls_total"]; !ok {
		t.Error("missing chronos_tool_calls_total")
	}
	if _, ok := r.gauges["chronos_active_sessions"]; !ok {
		t.Error("missing chronos_active_sessions")
	}
	if _, ok := r.histos["chronos_model_latency_seconds"]; !ok {
		t.Error("missing chronos_model_latency_seconds")
	}
}

func TestCounter_IncAndAdd(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("test_counter", "a test counter")
	c.Inc(nil)
	c.Inc(nil)
	c.Add(5, nil)
	if c.value != 7 {
		t.Errorf("value = %d, want 7", c.value)
	}
}

func TestCounter_Labels(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("labeled", "test")
	c.Inc(map[string]string{"method": "GET"})
	c.Inc(map[string]string{"method": "POST"})
	c.Inc(map[string]string{"method": "GET"})

	if c.labels[`method="GET"`] != 2 {
		t.Errorf("GET count = %d, want 2", c.labels[`method="GET"`])
	}
	if c.labels[`method="POST"`] != 1 {
		t.Errorf("POST count = %d, want 1", c.labels[`method="POST"`])
	}
}

func TestGauge_Set(t *testing.T) {
	r := NewRegistry()
	g := r.Gauge("test_gauge", "a test gauge")
	g.Set(42)
	if g.value != 42 {
		t.Errorf("value = %f, want 42", g.value)
	}
	g.Set(0)
	if g.value != 0 {
		t.Errorf("value = %f, want 0", g.value)
	}
}

func TestHistogram_Observe(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("test_histo", "a test histogram", []float64{0.1, 0.5, 1.0})
	h.Observe(0.05)
	h.Observe(0.3)
	h.Observe(0.8)
	h.Observe(2.0)

	if h.count != 4 {
		t.Errorf("count = %d, want 4", h.count)
	}
	if h.counts[0] != 1 { // <= 0.1
		t.Errorf("bucket[0.1] = %d, want 1", h.counts[0])
	}
	if h.counts[1] != 2 { // <= 0.5
		t.Errorf("bucket[0.5] = %d, want 2", h.counts[1])
	}
}

func TestHandler_PrometheusFormat(t *testing.T) {
	r := NewRegistry()
	r.IncAgentRuns("agent-1")
	r.IncToolCalls("calculator")
	r.SetActiveSessions(5)
	r.ObserveModelLatency("azure", 500*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)

	body := w.Body.String()

	if w.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("content type = %q", w.Header().Get("Content-Type"))
	}

	checks := []string{
		"# TYPE chronos_agent_runs_total counter",
		"chronos_agent_runs_total",
		"# TYPE chronos_active_sessions gauge",
		"chronos_active_sessions 5",
		"# TYPE chronos_model_latency_seconds histogram",
		"chronos_model_latency_seconds_count 1",
	}
	for _, check := range checks {
		if !strings.Contains(body, check) {
			t.Errorf("missing %q in output:\n%s", check, body)
		}
	}
}

func TestConvenienceMethods(t *testing.T) {
	r := NewRegistry()
	r.IncAgentRuns("a1")
	r.IncAgentRuns("a1")
	r.IncToolCalls("shell")
	r.AddTokens("openai", 1000)
	r.SetActiveSessions(3)
	r.ObserveModelLatency("azure", time.Second)

	if r.counters["chronos_agent_runs_total"].value != 2 {
		t.Errorf("agent runs = %d", r.counters["chronos_agent_runs_total"].value)
	}
	if r.counters["chronos_tool_calls_total"].value != 1 {
		t.Errorf("tool calls = %d", r.counters["chronos_tool_calls_total"].value)
	}
	if r.counters["chronos_tokens_used_total"].value != 1000 {
		t.Errorf("tokens = %d", r.counters["chronos_tokens_used_total"].value)
	}
	if r.gauges["chronos_active_sessions"].value != 3 {
		t.Errorf("sessions = %f", r.gauges["chronos_active_sessions"].value)
	}
}
