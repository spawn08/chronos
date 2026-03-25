package agent

import (
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

func TestChatSessionFromEvents_SkipsNonMapPayload(t *testing.T) {
	evts := []*storage.Event{
		{Type: "chat_message", Payload: "not-a-map"},
		{Type: "chat_message", Payload: map[string]any{"role": "user", "content": "ok"}},
	}
	cs := chatSessionFromEvents(evts)
	if len(cs.Messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(cs.Messages))
	}
	if cs.Messages[0].Content != "ok" {
		t.Errorf("content = %q", cs.Messages[0].Content)
	}
}

func TestChatSessionFromEvents_ChatSummaryStringPayloadIgnored(t *testing.T) {
	evts := []*storage.Event{
		{Type: "chat_summary", Payload: "wrong type"},
	}
	cs := chatSessionFromEvents(evts)
	if cs.Summary != "" {
		t.Error("string payload should not set summary")
	}
}

func TestChatSessionFromEvents_ToolCallsPartialRaw(t *testing.T) {
	evts := []*storage.Event{
		{
			Type: "chat_message",
			Payload: map[string]any{
				"role": "assistant",
				"tool_calls": []any{
					map[string]any{"id": "1", "name": "n", "arguments": "{}"},
					"skip-me",
				},
			},
		},
	}
	cs := chatSessionFromEvents(evts)
	if len(cs.Messages) != 1 {
		t.Fatalf("got %d messages", len(cs.Messages))
	}
	if len(cs.Messages[0].ToolCalls) != 1 {
		t.Errorf("tool calls = %d", len(cs.Messages[0].ToolCalls))
	}
}

func TestStrFromMap_MissingKey(t *testing.T) {
	if got := strFromMap(map[string]any{}, "missing"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestChatSessionFromEvents_UnknownEventType(t *testing.T) {
	evts := []*storage.Event{
		{Type: "other", Payload: map[string]any{"x": 1}},
	}
	cs := chatSessionFromEvents(evts)
	if len(cs.Messages) != 0 {
		t.Error("unknown types should be ignored")
	}
}

func TestCompressToolCalls_PreservesOrder(t *testing.T) {
	var msgs []model.Message
	msgs = append(msgs, model.Message{Role: model.RoleSystem, Content: "sys"})
	for i := 0; i < 4; i++ {
		msgs = append(msgs,
			model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: string(rune('a' + i)), Name: "t"}}},
			model.Message{Role: model.RoleTool, Content: "r", ToolCallID: string(rune('a' + i))},
		)
	}
	out := CompressToolCalls(msgs, 1)
	if len(out) >= len(msgs) {
		t.Fatal("expected compression")
	}
	first := out[0]
	if first.Role != model.RoleSystem {
		t.Errorf("first role = %s", first.Role)
	}
}
