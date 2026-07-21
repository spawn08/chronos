package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/hooks"
)

// Registry must satisfy the engine's MetricsSink so the integrator can wire it
// to a PrometheusHook without an adapter.
var _ hooks.MetricsSink = (*Registry)(nil)

// TestHistogram_CumulativeBuckets verifies the fixed bucket accounting: the
// le="x" bucket reports the cumulative count of observations <= x (not a
// double-accumulated value) and _sum/_count are correct.
func TestHistogram_CumulativeBuckets(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("lat", "latency", []float64{0.1, 0.5, 1.0})
	// observations: 0.05, 0.3, 0.8, 2.0
	for _, v := range []float64{0.05, 0.3, 0.8, 2.0} {
		h.Observe(v)
	}

	var b strings.Builder
	h.writeTo(&b)
	out := b.String()

	want := []string{
		`lat_bucket{le="0.1"} 1`,
		`lat_bucket{le="0.5"} 2`,
		`lat_bucket{le="1"} 3`,
		`lat_bucket{le="+Inf"} 4`,
		`lat_sum 3.15`,
		`lat_count 4`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

func TestRecordToolCall(t *testing.T) {
	tests := []struct {
		name  string
		calls []struct {
			tenant string
			tool   string
			dur    time.Duration
			isErr  bool
		}
		wantContains []string
	}{
		{
			name: "per-tenant tool calls and errors",
			calls: []struct {
				tenant string
				tool   string
				dur    time.Duration
				isErr  bool
			}{
				{"acme", "calculator", 20 * time.Millisecond, false},
				{"acme", "calculator", 30 * time.Millisecond, false},
				{"globex", "shell", 5 * time.Millisecond, true},
			},
			wantContains: []string{
				`chronos_tool_calls_total{tenant="acme",tool="calculator"} 2`,
				`chronos_tool_calls_total{tenant="globex",tool="shell"} 1`,
				`chronos_errors_total{kind="tool",tenant="globex"} 1`,
				`chronos_tool_latency_seconds_count{tenant="acme",tool="calculator"} 2`,
			},
		},
		{
			name: "empty tenant normalized to default",
			calls: []struct {
				tenant string
				tool   string
				dur    time.Duration
				isErr  bool
			}{
				{"", "web", time.Millisecond, false},
			},
			wantContains: []string{
				`chronos_tool_calls_total{tenant="default",tool="web"} 1`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			for _, c := range tt.calls {
				r.RecordToolCall(c.tenant, c.tool, c.dur, c.isErr)
			}
			out := serveMetrics(t, r)
			for _, w := range tt.wantContains {
				if !strings.Contains(out, w) {
					t.Errorf("missing %q in:\n%s", w, out)
				}
			}
		})
	}
}

func TestRecordModelCall(t *testing.T) {
	r := NewRegistry()
	r.RecordModelCall("acme", "azure", 500*time.Millisecond, 100, 50, false)
	r.RecordModelCall("acme", "azure", 700*time.Millisecond, 200, 80, true)

	out := serveMetrics(t, r)
	want := []string{
		`chronos_model_calls_total{provider="azure",tenant="acme"} 2`,
		`chronos_tokens_used_total{provider="azure",tenant="acme",type="prompt"} 300`,
		`chronos_tokens_used_total{provider="azure",tenant="acme",type="completion"} 130`,
		`chronos_errors_total{kind="model",tenant="acme"} 1`,
		`chronos_model_latency_seconds_count{provider="azure",tenant="acme"} 2`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in:\n%s", w, out)
		}
	}
}

// TestOTLPExporter_NoopWhenUnset ensures an empty endpoint never does I/O.
func TestOTLPExporter_NoopWhenUnset(t *testing.T) {
	e := NewOTLPExporter("")
	if e.Enabled() {
		t.Fatal("exporter with empty endpoint should be disabled")
	}
	r := NewRegistry()
	r.RecordToolCall("t", "tool", time.Millisecond, false)
	if err := e.Export(t.Context(), r); err != nil {
		t.Fatalf("no-op export returned error: %v", err)
	}
}

// TestOTLPExporter_Export posts OTLP JSON with the expected shape.
func TestOTLPExporter_Export(t *testing.T) {
	var gotBody []byte
	var gotPath, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotCT = req.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(req.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := NewRegistry()
	r.RecordModelCall("acme", "azure", 300*time.Millisecond, 10, 5, false)
	r.SetActiveSessions(3)

	e := NewOTLPExporter(srv.URL).WithHTTPClient(srv.Client())
	if err := e.Export(t.Context(), r); err != nil {
		t.Fatalf("Export: %v", err)
	}

	if gotPath != "/v1/metrics" {
		t.Errorf("path = %q, want /v1/metrics", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	body := string(gotBody)
	for _, w := range []string{"resourceMetrics", "chronos_model_calls_total", "chronos_active_sessions", "explicitBounds"} {
		if !strings.Contains(body, w) {
			t.Errorf("body missing %q:\n%s", w, body)
		}
	}
}

func TestBuildOTLPMetrics_HistogramBucketCounts(t *testing.T) {
	r := NewRegistry()
	h := r.Histogram("h", "h", []float64{0.1, 0.5, 1.0})
	for _, v := range []float64{0.05, 0.3, 0.8, 2.0} {
		h.Observe(v)
	}
	req := buildOTLPMetrics(r)
	var dp *otlpHistoDP
	for _, m := range req.ResourceMetrics[0].ScopeMetrics[0].Metrics {
		if m.Name == "h" && m.Histogram != nil && len(m.Histogram.DataPoints) > 0 {
			dp = &m.Histogram.DataPoints[0]
		}
	}
	if dp == nil {
		t.Fatal("histogram data point not found")
		return
	}
	// per-bucket (non-cumulative) counts: [0.05]=1, (0.1,0.5]=1, (0.5,1]=1, (1,+Inf)=1
	want := []uint64{1, 1, 1, 1}
	if len(dp.BucketCounts) != len(want) {
		t.Fatalf("bucketCounts len = %d, want %d", len(dp.BucketCounts), len(want))
	}
	for i := range want {
		if dp.BucketCounts[i] != want[i] {
			t.Errorf("bucketCounts[%d] = %d, want %d", i, dp.BucketCounts[i], want[i])
		}
	}
	if dp.Count != 4 {
		t.Errorf("count = %d, want 4", dp.Count)
	}
}

func serveMetrics(t *testing.T, r *Registry) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	return w.Body.String()
}
