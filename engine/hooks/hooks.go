// Package hooks provides a middleware system for intercepting agent execution events.
package hooks

import "context"

// EventType identifies the kind of execution event.
type EventType string

const (
	EventToolCallBefore  EventType = "tool_call.before"
	EventToolCallAfter   EventType = "tool_call.after"
	EventModelCallBefore EventType = "model_call.before"
	EventModelCallAfter  EventType = "model_call.after"
	EventNodeBefore      EventType = "node.before"
	EventNodeAfter       EventType = "node.after"

	EventContextOverflow EventType = "context.overflow"
	EventSummarization   EventType = "context.summarize"
	EventSessionStart    EventType = "session.start"
	EventSessionEnd      EventType = "session.end"
)

// Event carries data about an execution event.
type Event struct {
	Type     EventType      `json:"type"`
	Name     string         `json:"name"` // tool name, model name, or node ID
	Input    any            `json:"input,omitempty"`
	Output   any            `json:"output,omitempty"`
	Error    error          `json:"-"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Hook intercepts execution events for logging, metrics, or modification.
type Hook interface {
	// Before is called before the event executes. Return an error to abort.
	Before(ctx context.Context, evt *Event) error
	// After is called after the event executes.
	After(ctx context.Context, evt *Event) error
}

// Chain runs multiple hooks in sequence.
type Chain []Hook

func (c Chain) Before(ctx context.Context, evt *Event) error {
	for _, h := range c {
		if err := h.Before(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func (c Chain) After(ctx context.Context, evt *Event) error {
	// Run in reverse order for proper unwinding
	for i := len(c) - 1; i >= 0; i-- {
		if err := c[i].After(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

// LoggingHook is a simple hook that records events for observability.
type LoggingHook struct {
	Events []Event
}

func (h *LoggingHook) Before(_ context.Context, evt *Event) error {
	h.Events = append(h.Events, *evt)
	return nil
}

func (h *LoggingHook) After(_ context.Context, evt *Event) error {
	h.Events = append(h.Events, *evt)
	return nil
}
