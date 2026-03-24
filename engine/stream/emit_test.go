package stream

import (
	"context"
	"testing"
	"time"
)

func TestEmit_WithEmitter(t *testing.T) {
	ch := make(chan Event, 10)
	ctx := WithEmitter(context.Background(), ch)

	Emit(ctx, "progress", map[string]any{"pct": 50})

	select {
	case evt := <-ch:
		if evt.Type != EventCustom {
			t.Errorf("Type = %q, want %q", evt.Type, EventCustom)
		}
		data, ok := evt.Data.(map[string]any)
		if !ok {
			t.Fatalf("Data should be map[string]any, got %T", evt.Data)
		}
		if data["custom_type"] != "progress" {
			t.Errorf("custom_type = %v", data["custom_type"])
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event within 100ms")
	}
}

func TestEmit_WithoutEmitter(t *testing.T) {
	ctx := context.Background()
	Emit(ctx, "test", nil) // should not panic
}

func TestEmit_FullChannel(t *testing.T) {
	ch := make(chan Event) // unbuffered
	ctx := WithEmitter(context.Background(), ch)

	// Should not block even when channel is full
	done := make(chan struct{})
	go func() {
		Emit(ctx, "overflow", nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Emit should not block on full channel")
	}
}

func TestWithEmitter_PreservesParentContext(t *testing.T) {
	type myKey struct{}
	parent := context.WithValue(context.Background(), myKey{}, "hello")
	ch := make(chan Event, 1)
	ctx := WithEmitter(parent, ch)

	if ctx.Value(myKey{}) != "hello" {
		t.Error("parent context values should be preserved")
	}
}
