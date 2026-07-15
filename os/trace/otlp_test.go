package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOTelCollector_ExporterSelection(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantType string
	}{
		{"empty endpoint is noop", "", "trace.NoopSpanExporter"},
		{"endpoint wires otlp", "http://localhost:4318", "*trace.OTLPSpanExporter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewOTelCollector(tt.endpoint)
			got := typeName(c.exporter)
			if got != tt.wantType {
				t.Errorf("exporter type = %q, want %q", got, tt.wantType)
			}
		})
	}
}

// TestCollector_Export_NoopDefault verifies the default collector drains spans
// without touching the network.
func TestCollector_Export_NoopDefault(t *testing.T) {
	c := NewOTelCollector("")
	c.StartSpan(context.Background(), "op", "agent", nil)
	if err := c.Export(context.Background()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(c.Spans()) != 0 {
		t.Errorf("spans should be drained after Export, got %d", len(c.Spans()))
	}
}

func TestCollector_Export_EmptyIsNoError(t *testing.T) {
	c := NewOTelCollector("")
	if err := c.Export(context.Background()); err != nil {
		t.Fatalf("Export on empty collector: %v", err)
	}
}

func TestStdoutSpanExporter_WritesOTLPJSON(t *testing.T) {
	var buf bytes.Buffer
	c := NewOTelCollectorWithExporter(NewStdoutSpanExporter(&buf))

	ctx := WithTraceID(context.Background(), "trace_abc")
	span := c.StartSpan(ctx, "model_call", "model", map[string]any{"provider": "azure"})
	span.AddEvent("token_usage", map[string]any{"tokens": 42})
	c.EndSpan(span, nil)

	if err := c.Export(ctx); err != nil {
		t.Fatalf("Export: %v", err)
	}

	line := buf.String()
	if !strings.Contains(line, "resourceSpans") {
		t.Errorf("output missing resourceSpans: %s", line)
	}

	// Must be valid JSON.
	var req otlpTracesRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, line)
	}
	spans := req.ResourceSpans[0].ScopeSpans[0].Spans
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].TraceID != "trace_abc" {
		t.Errorf("traceId = %q, want trace_abc", spans[0].TraceID)
	}
	if spans[0].Status.Code != 1 {
		t.Errorf("status code = %d, want 1 (ok)", spans[0].Status.Code)
	}
	if len(spans[0].Events) != 1 || spans[0].Events[0].Name != "token_usage" {
		t.Errorf("expected token_usage event, got %+v", spans[0].Events)
	}
}

func TestOTLPSpanExporter_PostsToCollector(t *testing.T) {
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPath = req.URL.Path
		gotBody, _ = io.ReadAll(req.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp := NewOTLPSpanExporter(srv.URL).WithHTTPClient(srv.Client())
	c := NewOTelCollectorWithExporter(exp)
	span := c.StartSpan(context.Background(), "tool_call", "tool", nil)
	c.EndSpan(span, nil)

	if err := c.Export(context.Background()); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if gotPath != "/v1/traces" {
		t.Errorf("path = %q, want /v1/traces", gotPath)
	}
	if !strings.Contains(string(gotBody), "tool_call") {
		t.Errorf("body missing span name:\n%s", gotBody)
	}
}

func TestOTLPSpanExporter_EmptyEndpointNoop(t *testing.T) {
	exp := NewOTLPSpanExporter("")
	if err := exp.ExportSpans(context.Background(), []*OTelSpan{{Name: "x"}}); err != nil {
		t.Fatalf("empty-endpoint export should be noop, got %v", err)
	}
}

func TestNoopSpanExporter(t *testing.T) {
	var e NoopSpanExporter
	if err := e.ExportSpans(context.Background(), []*OTelSpan{{Name: "x"}}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	if err := e.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func TestStatusCode(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"ok", 1},
		{"error", 2},
		{"unset", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := statusCode(tt.in); got != tt.want {
			t.Errorf("statusCode(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case NoopSpanExporter:
		return "trace.NoopSpanExporter"
	case *OTLPSpanExporter:
		return "*trace.OTLPSpanExporter"
	case *StdoutSpanExporter:
		return "*trace.StdoutSpanExporter"
	default:
		return "unknown"
	}
}
