// Package agui exposes Chronos runs as a standard AG-UI event stream, so any
// AG-UI-compatible frontend can render an agent run (steps, tool calls, plan
// updates, state snapshots, interrupts, completion) without Chronos-specific
// glue. It is a translation layer over the existing engine/stream broker and its
// per-session SSE routing — the native /api/events/stream is unchanged; this
// adds a standardized stream alongside it.
package agui

import (
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/stream"
)

// EventType is an AG-UI protocol event type. The values follow the AG-UI
// convention (SCREAMING_SNAKE_CASE) so compatible frontends recognize them.
type EventType string

const (
	EventRunStarted       EventType = "RUN_STARTED"
	EventRunFinished      EventType = "RUN_FINISHED"
	EventRunError         EventType = "RUN_ERROR"
	EventStepStarted      EventType = "STEP_STARTED"
	EventStepFinished     EventType = "STEP_FINISHED"
	EventTextMessageStart EventType = "TEXT_MESSAGE_START"
	EventTextMessageChunk EventType = "TEXT_MESSAGE_CONTENT"
	EventTextMessageEnd   EventType = "TEXT_MESSAGE_END"
	EventToolCallStart    EventType = "TOOL_CALL_START"
	EventToolCallArgs     EventType = "TOOL_CALL_ARGS"
	EventToolCallEnd      EventType = "TOOL_CALL_END"
	EventToolCallResult   EventType = "TOOL_CALL_RESULT"
	EventStateSnapshot    EventType = "STATE_SNAPSHOT"
	EventCustom           EventType = "CUSTOM"
)

// Event is one AG-UI protocol event. Fields are a superset across event types;
// only those relevant to Type are populated (the rest are omitted on the wire).
type Event struct {
	Type EventType `json:"type"`

	ThreadID string `json:"threadId,omitempty"`
	RunID    string `json:"runId,omitempty"`

	StepName string `json:"stepName,omitempty"`

	MessageID string `json:"messageId,omitempty"`
	Role      string `json:"role,omitempty"`

	ToolCallID   string `json:"toolCallId,omitempty"`
	ToolCallName string `json:"toolCallName,omitempty"`
	Delta        string `json:"delta,omitempty"`
	Content      string `json:"content,omitempty"`

	Snapshot any `json:"snapshot,omitempty"`

	// CUSTOM events carry a name and an arbitrary value.
	Name  string `json:"name,omitempty"`
	Value any    `json:"value,omitempty"`

	// RUN_ERROR carries a message.
	Message string `json:"message,omitempty"`
}

// Translator converts Chronos stream events into AG-UI events for one stream
// connection. It holds the per-connection state AG-UI needs: the thread/run ids
// and a monotonic tool-call id so TOOL_CALL_* events correlate. It is not safe
// for concurrent use; use one per SSE connection.
type Translator struct {
	threadID string
	runID    string

	toolSeq    int
	lastToolID string

	msgSeq    int
	openMsgID string // non-empty while an assistant TEXT_MESSAGE is open
}

// NewTranslator creates a translator for a stream scoped to threadID (typically
// the session id). runID identifies this run within the thread.
func NewTranslator(threadID, runID string) *Translator {
	return &Translator{threadID: threadID, runID: runID}
}

// Start returns the RUN_STARTED event that opens the stream. The handler emits
// it on connect (not lazily on the first Chronos event) so a client sees a live,
// well-formed lifecycle immediately, even before the run produces any event.
func (t *Translator) Start() Event {
	return Event{Type: EventRunStarted, ThreadID: t.threadID, RunID: t.runID}
}

// Translate maps one Chronos stream event to zero or more AG-UI events. Unmapped
// Chronos events (e.g. edge transitions, raw model-call notifications) translate
// to nothing.
func (t *Translator) Translate(evt stream.Event) []Event {
	m := normalize(evt.Data)
	switch evt.Type {
	case stream.EventNodeStart:
		return []Event{{Type: EventStepStarted, StepName: str(m, "node_id")}}
	case stream.EventNodeEnd:
		return []Event{{Type: EventStepFinished, StepName: str(m, "node_id")}}
	case stream.EventModelDelta:
		return t.textDelta(str(m, "content"))
	case stream.EventModelResponse:
		// Streaming: closes the open assistant message. Non-streaming: the event
		// carries the whole content, emitted as a complete message.
		return t.textFromResponse(str(m, "content"))
	case stream.EventToolCall:
		id := t.toolID(str(m, "id"))
		return []Event{
			{Type: EventToolCallStart, ToolCallID: id, ToolCallName: str(m, "tool")},
			{Type: EventToolCallArgs, ToolCallID: id, Delta: jsonString(m["args"])},
			{Type: EventToolCallEnd, ToolCallID: id},
		}
	case stream.EventToolResult:
		content := str(m, "result")
		if content == "" {
			content = str(m, "error")
		}
		if content == "" {
			content = jsonString(m["result"])
		}
		return t.toolResult(str(m, "id"), str(m, "tool"), content)
	case stream.EventPlanUpdate:
		return []Event{{Type: EventCustom, Name: "plan", Value: m}}
	case stream.EventCheckpoint:
		return []Event{{Type: EventStateSnapshot, Snapshot: m["state"]}}
	case stream.EventInterrupt:
		return []Event{{Type: EventCustom, Name: "interrupt", Value: map[string]any{"node": str(m, "node_id")}}}
	case stream.EventCompleted:
		return []Event{{Type: EventRunFinished, ThreadID: t.threadID, RunID: t.runID}}
	case stream.EventError:
		return []Event{{Type: EventRunError, Message: str(m, "error")}}
	case stream.EventCustom:
		name := str(m, "name")
		if name == "" {
			name = "custom"
		}
		return []Event{{Type: EventCustom, Name: name, Value: m}}
	default:
		// model_call, edge_transition, checkpoint-less events: nothing to render.
		return nil
	}
}

// textDelta maps a streamed token into TEXT_MESSAGE events, opening a message on
// the first delta so START precedes any CONTENT.
func (t *Translator) textDelta(content string) []Event {
	if content == "" {
		return nil
	}
	var out []Event
	if t.openMsgID == "" {
		t.openMsgID = t.nextMsgID()
		out = append(out, Event{Type: EventTextMessageStart, MessageID: t.openMsgID, Role: "assistant"})
	}
	return append(out, Event{Type: EventTextMessageChunk, MessageID: t.openMsgID, Delta: content})
}

// textFromResponse closes an open streamed message, or — when the response
// carries the full content in one shot (non-streaming path) — emits a complete
// START→CONTENT→END message. A response with no content and no open message
// yields nothing.
func (t *Translator) textFromResponse(content string) []Event {
	if t.openMsgID != "" {
		id := t.openMsgID
		t.openMsgID = ""
		return []Event{{Type: EventTextMessageEnd, MessageID: id}}
	}
	if content == "" {
		return nil
	}
	id := t.nextMsgID()
	return []Event{
		{Type: EventTextMessageStart, MessageID: id, Role: "assistant"},
		{Type: EventTextMessageChunk, MessageID: id, Delta: content},
		{Type: EventTextMessageEnd, MessageID: id},
	}
}

// toolID returns the id to use for a tool call: the source id when the producer
// supplied one (stable, collision-free correlation), else a synthesized
// monotonic id. It records the result so a following TOOL_CALL_RESULT correlates.
func (t *Translator) toolID(sourceID string) string {
	if sourceID != "" {
		t.lastToolID = sourceID
		return sourceID
	}
	t.toolSeq++
	t.lastToolID = fmt.Sprintf("%s-tool-%d", t.runID, t.toolSeq)
	return t.lastToolID
}

// toolResult emits a TOOL_CALL_RESULT correlated to its call. When this
// connection never saw the matching call (a mid-run or firehose subscriber), it
// first synthesizes a TOOL_CALL_START so the result is never an orphan the
// frontend can't attach to a call.
func (t *Translator) toolResult(sourceID, toolName, content string) []Event {
	id := sourceID
	if id == "" {
		id = t.lastToolID
	}
	if id == "" {
		// No call seen on this connection: open and close one so the result is a
		// well-formed START→END→RESULT rather than a correlation-less orphan.
		id = t.toolID("")
		return []Event{
			{Type: EventToolCallStart, ToolCallID: id, ToolCallName: toolName},
			{Type: EventToolCallEnd, ToolCallID: id},
			{Type: EventToolCallResult, ToolCallID: id, Content: content},
		}
	}
	return []Event{{Type: EventToolCallResult, ToolCallID: id, Content: content}}
}

// nextMsgID returns a fresh assistant message id for this connection.
func (t *Translator) nextMsgID() string {
	t.msgSeq++
	return fmt.Sprintf("%s-msg-%d", t.runID, t.msgSeq)
}

// normalize renders a heterogeneous stream event payload (a graph.StreamEvent
// struct from the runner, or a map[string]any from the agent) as a uniform map
// via a JSON round-trip, so field access is the same regardless of source.
func normalize(data any) map[string]any {
	if data == nil {
		return map[string]any{}
	}
	if m, ok := data.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(data)
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// str reads a string field from a normalized map, tolerating absence.
func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// jsonString renders v as compact JSON (empty string for nil), used for the
// tool-args delta and text fallbacks.
func jsonString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
