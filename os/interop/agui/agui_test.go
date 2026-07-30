package agui

import (
	"testing"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/stream"
)

// types extracts just the AG-UI event types from a translation, for compact
// assertions.
func types(evts []Event) []EventType {
	out := make([]EventType, len(evts))
	for i := range evts {
		out[i] = evts[i].Type
	}
	return out
}

func eq(a, b []EventType) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestTranslator_MapsEventTypes(t *testing.T) {
	tests := []struct {
		name string
		evt  stream.Event
		want []EventType
	}{
		{
			name: "node_start -> STEP_STARTED (from runner struct payload)",
			evt:  stream.Event{Type: stream.EventNodeStart, Data: graph.StreamEvent{Type: "node_start", NodeID: "work"}},
			want: []EventType{EventStepStarted},
		},
		{
			name: "tool_call -> START/ARGS/END",
			evt:  stream.Event{Type: stream.EventToolCall, Data: map[string]any{"tool": "calc", "args": map[string]any{"x": 1}}},
			want: []EventType{EventToolCallStart, EventToolCallArgs, EventToolCallEnd},
		},
		{
			name: "orphan tool_result -> synthesized START + END + RESULT",
			evt:  stream.Event{Type: stream.EventToolResult, Data: map[string]any{"tool": "calc", "result": "3"}},
			want: []EventType{EventToolCallStart, EventToolCallEnd, EventToolCallResult},
		},
		{
			name: "plan_update -> CUSTOM",
			evt:  stream.Event{Type: stream.EventPlanUpdate, Data: map[string]any{"summary": "x", "complete": false}},
			want: []EventType{EventCustom},
		},
		{
			name: "checkpoint -> STATE_SNAPSHOT",
			evt:  stream.Event{Type: stream.EventCheckpoint, Data: map[string]any{"state": map[string]any{"k": "v"}}},
			want: []EventType{EventStateSnapshot},
		},
		{
			name: "interrupt -> CUSTOM",
			evt:  stream.Event{Type: stream.EventInterrupt, Data: map[string]any{"node_id": "gate"}},
			want: []EventType{EventCustom},
		},
		{
			name: "completed -> RUN_FINISHED",
			evt:  stream.Event{Type: stream.EventCompleted, Data: map[string]any{}},
			want: []EventType{EventRunFinished},
		},
		{
			name: "error -> RUN_ERROR",
			evt:  stream.Event{Type: stream.EventError, Data: map[string]any{"error": "boom"}},
			want: []EventType{EventRunError},
		},
		{
			name: "model_call -> nothing",
			evt:  stream.Event{Type: stream.EventModelCall, Data: map[string]any{"model": "x"}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := NewTranslator("thread-1", "run-1")
			got := tr.Translate(tt.evt)
			if !eq(types(got), tt.want) {
				t.Errorf("types = %v, want %v", types(got), tt.want)
			}
		})
	}
}

func TestTranslator_StartEmitsRunStartedWithIDs(t *testing.T) {
	tr := NewTranslator("thread-42", "run-99")
	start := tr.Start()
	if start.Type != EventRunStarted || start.ThreadID != "thread-42" || start.RunID != "run-99" {
		t.Errorf("Start() = %+v, want RUN_STARTED with the thread/run ids", start)
	}
}

func TestTranslator_ToolCallCorrelation(t *testing.T) {
	tr := NewTranslator("t", "run7")
	call := tr.Translate(stream.Event{Type: stream.EventToolCall, Data: map[string]any{"tool": "calc", "args": map[string]any{"x": 1}}})
	if call[0].Type != EventToolCallStart || call[0].ToolCallName != "calc" {
		t.Fatalf("start = %+v", call[0])
	}
	id := call[0].ToolCallID
	if id == "" {
		t.Fatal("tool call id must be set")
	}
	if call[1].ToolCallID != id || call[1].Delta != `{"x":1}` {
		t.Errorf("args = %+v, want same id %q and json delta", call[1], id)
	}
	if call[2].ToolCallID != id {
		t.Errorf("end id = %q, want %q", call[2].ToolCallID, id)
	}

	// A following tool_result references the same id.
	res := tr.Translate(stream.Event{Type: stream.EventToolResult, Data: map[string]any{"result": "3"}})
	if res[0].ToolCallID != id || res[0].Content != "3" {
		t.Errorf("result = %+v, want id %q content 3", res[0], id)
	}
}

func TestTranslator_StreamingText(t *testing.T) {
	tr := NewTranslator("t", "r")
	// First delta opens the message.
	d1 := tr.Translate(stream.Event{Type: stream.EventModelDelta, Data: map[string]any{"content": "Hel"}})
	if !eq(types(d1), []EventType{EventTextMessageStart, EventTextMessageChunk}) {
		t.Fatalf("first delta = %v, want START,CONTENT", types(d1))
	}
	msgID := d1[0].MessageID
	if msgID == "" || d1[1].Delta != "Hel" {
		t.Fatalf("delta events = %+v", d1)
	}
	// Subsequent delta reuses the open message (no new START).
	d2 := tr.Translate(stream.Event{Type: stream.EventModelDelta, Data: map[string]any{"content": "lo"}})
	if !eq(types(d2), []EventType{EventTextMessageChunk}) || d2[0].MessageID != msgID {
		t.Fatalf("second delta = %+v, want a CONTENT on the same message", d2)
	}
	// The response closes the message.
	end := tr.Translate(stream.Event{Type: stream.EventModelResponse, Data: map[string]any{"stop_reason": "end"}})
	if !eq(types(end), []EventType{EventTextMessageEnd}) || end[0].MessageID != msgID {
		t.Fatalf("response = %+v, want END on the same message", end)
	}
}

func TestTranslator_NonStreamingText(t *testing.T) {
	tr := NewTranslator("t", "r")
	// A response with full content and no prior deltas is a complete message.
	got := tr.Translate(stream.Event{Type: stream.EventModelResponse, Data: map[string]any{"content": "hi there"}})
	if !eq(types(got), []EventType{EventTextMessageStart, EventTextMessageChunk, EventTextMessageEnd}) {
		t.Fatalf("non-streaming response = %v, want START,CONTENT,END", types(got))
	}
	if got[1].Delta != "hi there" {
		t.Errorf("content = %q, want 'hi there'", got[1].Delta)
	}
	// An empty response with nothing open yields nothing.
	if evs := tr.Translate(stream.Event{Type: stream.EventModelResponse, Data: map[string]any{}}); len(evs) != 0 {
		t.Errorf("empty response → %v, want nothing", types(evs))
	}
}

func TestTranslator_ToolCallUsesSourceID(t *testing.T) {
	tr := NewTranslator("t", "r")
	got := tr.Translate(stream.Event{Type: stream.EventToolCall, Data: map[string]any{"id": "call_abc", "tool": "calc"}})
	if got[0].ToolCallID != "call_abc" {
		t.Errorf("tool call id = %q, want the source id call_abc", got[0].ToolCallID)
	}
	res := tr.Translate(stream.Event{Type: stream.EventToolResult, Data: map[string]any{"id": "call_abc", "result": "3"}})
	if res[0].ToolCallID != "call_abc" {
		t.Errorf("result id = %q, want call_abc", res[0].ToolCallID)
	}
}

// A tool_result seen with no matching tool_call on this connection (mid-run or
// firehose subscribe) must not be an orphan: a TOOL_CALL_START is synthesized so
// the result correlates. Guards BLOCK-Q01.
func TestTranslator_OrphanToolResultGetsStart(t *testing.T) {
	tr := NewTranslator("t", "r")
	got := tr.Translate(stream.Event{Type: stream.EventToolResult, Data: map[string]any{"tool": "calc", "result": "3"}})
	if !eq(types(got), []EventType{EventToolCallStart, EventToolCallEnd, EventToolCallResult}) {
		t.Fatalf("orphan result = %v, want START, END, RESULT", types(got))
	}
	if got[0].ToolCallID == "" || got[0].ToolCallID != got[2].ToolCallID {
		t.Errorf("synthesized start/result ids must match and be non-empty: %+v", got)
	}
	if got[0].ToolCallName != "calc" {
		t.Errorf("synthesized start name = %q, want calc", got[0].ToolCallName)
	}
}

func TestTranslator_CompletedCarriesIDs(t *testing.T) {
	tr := NewTranslator("thread-42", "run-99")
	got := tr.Translate(stream.Event{Type: stream.EventCompleted, Data: map[string]any{}})
	if len(got) != 1 || got[0].Type != EventRunFinished || got[0].ThreadID != "thread-42" || got[0].RunID != "run-99" {
		t.Errorf("completed → %+v, want a single RUN_FINISHED carrying the ids", got)
	}
}
