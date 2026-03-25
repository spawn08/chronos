package trace

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// OTelSpan represents an OpenTelemetry-compatible span.
type OTelSpan struct {
	TraceID    string         `json:"trace_id"`
	SpanID     string         `json:"span_id"`
	ParentID   string         `json:"parent_id,omitempty"`
	Name       string         `json:"name"`
	Kind       string         `json:"kind"` // agent, graph, tool, model
	Attributes map[string]any `json:"attributes,omitempty"`
	StartTime  time.Time      `json:"start_time"`
	EndTime    time.Time      `json:"end_time,omitempty"`
	Status     string         `json:"status"` // ok, error, unset
	Events     []SpanEvent    `json:"events,omitempty"`
}

// SpanEvent is a timestamped annotation on a span.
type SpanEvent struct {
	Name       string         `json:"name"`
	Timestamp  time.Time      `json:"timestamp"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// OTelCollector collects OpenTelemetry-compatible spans and exports them
// to a configured endpoint. It supports proper parent-child relationships
// and attributes for agent, graph, tool, and model operations.
type OTelCollector struct {
	mu       sync.Mutex
	spans    []*OTelSpan
	endpoint string
	enabled  bool
	counter  int64
}

// NewOTelCollector creates a new OTel-compatible span collector.
// endpoint is the OTLP endpoint to export spans to (empty = collect only, no export).
func NewOTelCollector(endpoint string) *OTelCollector {
	return &OTelCollector{
		endpoint: endpoint,
		enabled:  true,
	}
}

// SetEnabled enables or disables span collection.
func (c *OTelCollector) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
}

// StartSpan begins a new OTel span with the given name and kind.
func (c *OTelCollector) StartSpan(ctx context.Context, name, kind string, attrs map[string]any) *OTelSpan {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return &OTelSpan{Name: name, Kind: kind, Status: "unset"}
	}

	c.counter++
	span := &OTelSpan{
		TraceID:    traceIDFromContext(ctx),
		SpanID:     fmt.Sprintf("span_%d_%d", time.Now().UnixNano(), c.counter),
		ParentID:   parentSpanFromContext(ctx),
		Name:       name,
		Kind:       kind,
		Attributes: attrs,
		StartTime:  time.Now(),
		Status:     "unset",
	}
	c.spans = append(c.spans, span)
	return span
}

// EndSpan completes a span, setting its end time and status.
func (c *OTelCollector) EndSpan(span *OTelSpan, err error) {
	if span == nil {
		return
	}
	span.EndTime = time.Now()
	if err != nil {
		span.Status = "error"
		span.AddEvent("exception", map[string]any{"message": err.Error()})
	} else {
		span.Status = "ok"
	}
}

// AddEvent adds a timestamped event to a span.
func (span *OTelSpan) AddEvent(name string, attrs map[string]any) {
	if span == nil {
		return
	}
	span.Events = append(span.Events, SpanEvent{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrs,
	})
}

// Spans returns all collected spans.
func (c *OTelCollector) Spans() []*OTelSpan {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*OTelSpan, len(c.spans))
	copy(result, c.spans)
	return result
}

// Flush returns all spans and clears the collector.
func (c *OTelCollector) Flush() []*OTelSpan {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.spans
	c.spans = nil
	return result
}

// contextKey type for trace context propagation.
type contextKey string

const (
	traceIDKey    contextKey = "chronos_trace_id"
	parentSpanKey contextKey = "chronos_parent_span"
)

// WithTraceID adds a trace ID to the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// WithParentSpan adds a parent span ID to the context.
func WithParentSpan(ctx context.Context, spanID string) context.Context {
	return context.WithValue(ctx, parentSpanKey, spanID)
}

func traceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return fmt.Sprintf("trace_%d", time.Now().UnixNano())
}

func parentSpanFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(parentSpanKey).(string); ok {
		return v
	}
	return ""
}
