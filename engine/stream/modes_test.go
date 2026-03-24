package stream

import "testing"

func TestStreamConfig_ShouldInclude_Debug(t *testing.T) {
	cfg := StreamConfig{Mode: ModeDebug}
	events := []string{EventNodeStart, EventNodeEnd, EventToolCall, EventCustom, EventModelCall, EventCompleted}
	for _, e := range events {
		if !cfg.ShouldInclude(e) {
			t.Errorf("debug mode should include %q", e)
		}
	}
}

func TestStreamConfig_ShouldInclude_Values(t *testing.T) {
	cfg := StreamConfig{Mode: ModeValues}

	if !cfg.ShouldInclude(EventNodeEnd) {
		t.Error("values mode should include node_end")
	}
	if !cfg.ShouldInclude(EventCompleted) {
		t.Error("values mode should include completed")
	}
	if cfg.ShouldInclude(EventNodeStart) {
		t.Error("values mode should not include node_start")
	}
	if cfg.ShouldInclude(EventToolCall) {
		t.Error("values mode should not include tool_call")
	}
}

func TestStreamConfig_ShouldInclude_Custom(t *testing.T) {
	cfg := StreamConfig{Mode: ModeCustom}

	if !cfg.ShouldInclude(EventCustom) {
		t.Error("custom mode should include custom events")
	}
	if cfg.ShouldInclude(EventNodeEnd) {
		t.Error("custom mode should not include node_end")
	}
}

func TestStreamConfig_ShouldInclude_Messages(t *testing.T) {
	cfg := StreamConfig{Mode: ModeMessages}

	if !cfg.ShouldInclude(EventModelCall) {
		t.Error("messages mode should include model_call")
	}
	if !cfg.ShouldInclude(EventModelResponse) {
		t.Error("messages mode should include model_response")
	}
	if cfg.ShouldInclude(EventNodeStart) {
		t.Error("messages mode should not include node_start")
	}
}

func TestStreamConfig_ShouldInclude_Updates(t *testing.T) {
	cfg := StreamConfig{Mode: ModeUpdates}

	if !cfg.ShouldInclude(EventNodeStart) {
		t.Error("updates mode should include node_start")
	}
	if !cfg.ShouldInclude(EventNodeEnd) {
		t.Error("updates mode should include node_end")
	}
	if cfg.ShouldInclude(EventToolCall) {
		t.Error("updates mode should not include tool_call")
	}
}

func TestDefaultStreamConfig(t *testing.T) {
	cfg := DefaultStreamConfig()
	if cfg.Mode != ModeDebug {
		t.Errorf("default mode should be debug, got %q", cfg.Mode)
	}
}
