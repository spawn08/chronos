package trace

import (
	"context"
	"fmt"
	"testing"
)

func TestOTelCollector_StartEndSpan(t *testing.T) {
	c := NewOTelCollector("")
	ctx := context.Background()

	span := c.StartSpan(ctx, "test_op", "agent", map[string]any{"agent_id": "a1"})
	if span.Name != "test_op" {
		t.Errorf("name = %q, want test_op", span.Name)
	}
	if span.Kind != "agent" {
		t.Errorf("kind = %q, want agent", span.Kind)
	}
	if span.Status != "unset" {
		t.Errorf("status = %q, want unset", span.Status)
	}

	c.EndSpan(span, nil)
	if span.Status != "ok" {
		t.Errorf("status after end = %q, want ok", span.Status)
	}
	if span.EndTime.IsZero() {
		t.Error("end time should be set")
	}
}

func TestOTelCollector_ErrorSpan(t *testing.T) {
	c := NewOTelCollector("")
	span := c.StartSpan(context.Background(), "fail_op", "tool", nil)
	c.EndSpan(span, fmt.Errorf("something broke"))

	if span.Status != "error" {
		t.Errorf("status = %q, want error", span.Status)
	}
	if len(span.Events) != 1 || span.Events[0].Name != "exception" {
		t.Error("expected exception event")
	}
}

func TestOTelCollector_AddEvent(t *testing.T) {
	c := NewOTelCollector("")
	span := c.StartSpan(context.Background(), "op", "model", nil)
	span.AddEvent("token_usage", map[string]any{"tokens": 100})
	c.EndSpan(span, nil)

	if len(span.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(span.Events))
	}
	if span.Events[0].Name != "token_usage" {
		t.Errorf("event name = %q", span.Events[0].Name)
	}
}

func TestOTelCollector_ParentChild(t *testing.T) {
	c := NewOTelCollector("")
	ctx := WithTraceID(context.Background(), "trace_123")

	parent := c.StartSpan(ctx, "parent", "agent", nil)
	childCtx := WithParentSpan(ctx, parent.SpanID)
	child := c.StartSpan(childCtx, "child", "tool", nil)

	if child.ParentID != parent.SpanID {
		t.Errorf("child parent = %q, want %q", child.ParentID, parent.SpanID)
	}
	if child.TraceID != "trace_123" {
		t.Errorf("child trace = %q, want trace_123", child.TraceID)
	}
}

func TestOTelCollector_Flush(t *testing.T) {
	c := NewOTelCollector("")
	c.StartSpan(context.Background(), "a", "agent", nil)
	c.StartSpan(context.Background(), "b", "tool", nil)

	spans := c.Flush()
	if len(spans) != 2 {
		t.Errorf("flush returned %d spans, want 2", len(spans))
	}
	if len(c.Spans()) != 0 {
		t.Error("spans should be empty after flush")
	}
}

func TestOTelCollector_Disabled(t *testing.T) {
	c := NewOTelCollector("")
	c.SetEnabled(false)

	span := c.StartSpan(context.Background(), "op", "agent", nil)
	// Disabled collector still returns a span (for nil safety) but doesn't track it
	if span.Name != "op" {
		t.Errorf("name = %q", span.Name)
	}
	if len(c.Spans()) != 0 {
		t.Error("disabled collector should not track spans")
	}
}

func TestOTelCollector_NilSpan(t *testing.T) {
	c := NewOTelCollector("")
	// Should not panic
	c.EndSpan(nil, nil)
	var span *OTelSpan
	span.AddEvent("test", nil)
}

func TestContextPropagation(t *testing.T) {
	ctx := context.Background()
	ctx = WithTraceID(ctx, "t1")
	ctx = WithParentSpan(ctx, "s1")

	if traceIDFromContext(ctx) != "t1" {
		t.Errorf("trace id = %q", traceIDFromContext(ctx))
	}
	if parentSpanFromContext(ctx) != "s1" {
		t.Errorf("parent span = %q", parentSpanFromContext(ctx))
	}
}
