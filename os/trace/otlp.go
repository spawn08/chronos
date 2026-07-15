package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SpanExporter exports collected spans to a backend. Implementations must be
// safe for offline use: with no endpoint configured the collector uses a
// NoopSpanExporter so tests and disconnected runs never require a collector.
type SpanExporter interface {
	// ExportSpans sends a batch of spans. It should honor ctx cancellation.
	ExportSpans(ctx context.Context, spans []*OTelSpan) error
	// Shutdown flushes and releases any resources.
	Shutdown(ctx context.Context) error
}

// NoopSpanExporter discards spans. It is the default when no OTLP endpoint is
// configured.
type NoopSpanExporter struct{}

func (NoopSpanExporter) ExportSpans(context.Context, []*OTelSpan) error { return nil }
func (NoopSpanExporter) Shutdown(context.Context) error                 { return nil }

// StdoutSpanExporter writes each span batch as OTLP/JSON to an io.Writer,
// useful for local debugging without a collector.
type StdoutSpanExporter struct {
	w io.Writer
}

// NewStdoutSpanExporter creates a stdout-style exporter writing to w.
func NewStdoutSpanExporter(w io.Writer) *StdoutSpanExporter {
	return &StdoutSpanExporter{w: w}
}

func (e *StdoutSpanExporter) ExportSpans(_ context.Context, spans []*OTelSpan) error {
	if e.w == nil || len(spans) == 0 {
		return nil
	}
	payload := buildOTLPTraces(spans)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal otlp traces: %w", err)
	}
	if _, err := e.w.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write spans: %w", err)
	}
	return nil
}

func (e *StdoutSpanExporter) Shutdown(context.Context) error { return nil }

// OTLPSpanExporter exports spans to an OTLP/HTTP endpoint using the JSON
// encoding of the OpenTelemetry trace protocol (ExportTraceServiceRequest).
type OTLPSpanExporter struct {
	endpoint string
	client   *http.Client
	headers  map[string]string
}

// NewOTLPSpanExporter creates an exporter that POSTs to "<endpoint>/v1/traces".
func NewOTLPSpanExporter(endpoint string) *OTLPSpanExporter {
	return &OTLPSpanExporter{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 10 * time.Second},
		headers:  map[string]string{},
	}
}

// WithHTTPClient overrides the HTTP client (useful for tests).
func (e *OTLPSpanExporter) WithHTTPClient(c *http.Client) *OTLPSpanExporter {
	e.client = c
	return e
}

// WithHeader adds a header sent with each export request.
func (e *OTLPSpanExporter) WithHeader(k, v string) *OTLPSpanExporter {
	e.headers[k] = v
	return e
}

func (e *OTLPSpanExporter) ExportSpans(ctx context.Context, spans []*OTelSpan) error {
	if e.endpoint == "" || len(spans) == 0 {
		return nil
	}
	payload := buildOTLPTraces(spans)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal otlp traces: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint+"/v1/traces", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build otlp traces request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("export otlp traces: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("export otlp traces: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (e *OTLPSpanExporter) Shutdown(context.Context) error { return nil }

// --- Minimal OTLP/JSON trace structures ---

type otlpTracesRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpScopeSpans struct {
	Spans []otlpSpan `json:"spans"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId,omitempty"`
	Name              string          `json:"name"`
	Kind              int             `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano,omitempty"`
	Attributes        []otlpKV        `json:"attributes,omitempty"`
	Events            []otlpSpanEvent `json:"events,omitempty"`
	Status            otlpStatus      `json:"status"`
}

type otlpSpanEvent struct {
	Name         string   `json:"name"`
	TimeUnixNano string   `json:"timeUnixNano"`
	Attributes   []otlpKV `json:"attributes,omitempty"`
}

type otlpStatus struct {
	Code int `json:"code"`
}

type otlpKV struct {
	Key   string      `json:"key"`
	Value otlpKVValue `json:"value"`
}

type otlpKVValue struct {
	StringValue string `json:"stringValue"`
}

func unixNano(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d", t.UnixNano())
}

// statusCode maps the collector's string status to the OTLP status code enum
// (0=unset, 1=ok, 2=error).
func statusCode(s string) int {
	switch s {
	case "ok":
		return 1
	case "error":
		return 2
	default:
		return 0
	}
}

func attrsFromMap(m map[string]any) []otlpKV {
	if len(m) == 0 {
		return nil
	}
	out := make([]otlpKV, 0, len(m))
	for k, v := range m {
		out = append(out, otlpKV{Key: k, Value: otlpKVValue{StringValue: fmt.Sprintf("%v", v)}})
	}
	return out
}

// buildOTLPTraces converts collected spans into an OTLP trace request.
func buildOTLPTraces(spans []*OTelSpan) otlpTracesRequest {
	out := make([]otlpSpan, 0, len(spans))
	for _, s := range spans {
		if s == nil {
			continue
		}
		events := make([]otlpSpanEvent, 0, len(s.Events))
		for _, ev := range s.Events {
			events = append(events, otlpSpanEvent{
				Name:         ev.Name,
				TimeUnixNano: unixNano(ev.Timestamp),
				Attributes:   attrsFromMap(ev.Attributes),
			})
		}
		out = append(out, otlpSpan{
			TraceID:           s.TraceID,
			SpanID:            s.SpanID,
			ParentSpanID:      s.ParentID,
			Name:              s.Name,
			Kind:              1, // SPAN_KIND_INTERNAL
			StartTimeUnixNano: unixNano(s.StartTime),
			EndTimeUnixNano:   unixNano(s.EndTime),
			Attributes:        attrsFromMap(s.Attributes),
			Events:            events,
			Status:            otlpStatus{Code: statusCode(s.Status)},
		})
	}
	return otlpTracesRequest{
		ResourceSpans: []otlpResourceSpans{{
			ScopeSpans: []otlpScopeSpans{{Spans: out}},
		}},
	}
}
