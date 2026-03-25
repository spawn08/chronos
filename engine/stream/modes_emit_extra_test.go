package stream

import (
	"context"
	"testing"
)

func TestStreamConfig_ShouldInclude_UnknownMode(t *testing.T) {
	cfg := StreamConfig{Mode: StreamMode("totally-unknown-mode")}
	if !cfg.ShouldInclude("any_event_type") {
		t.Error("unknown mode should include events (passthrough)")
	}
	if !cfg.ShouldInclude(EventToolCall) {
		t.Error("unknown mode should not filter")
	}
}

func TestStreamConfig_ShouldInclude_ModeValuesExcludesCheckpoint(t *testing.T) {
	cfg := StreamConfig{Mode: ModeValues}
	if cfg.ShouldInclude(EventCheckpoint) {
		t.Error("values mode should not include checkpoint")
	}
}

func TestStreamConfig_ShouldInclude_ModeUpdatesIncludesEdgeCases(t *testing.T) {
	cfg := StreamConfig{Mode: ModeUpdates}
	if cfg.ShouldInclude(EventError) {
		t.Error("updates mode should not include error by default")
	}
}

func TestEmit_WrongContextValueType(t *testing.T) {
	ctx := context.WithValue(context.Background(), emitKey, "not-a-channel")
	Emit(ctx, "x", nil) // must not panic
}

func TestEmit_NilChannelInterface(t *testing.T) {
	var ch chan<- Event
	ctx := context.WithValue(context.Background(), emitKey, any(ch))
	Emit(ctx, "x", nil)
}
