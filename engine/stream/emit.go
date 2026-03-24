package stream

import "context"

type emitKeyType struct{}

var emitKey = emitKeyType{}

// WithEmitter adds an event emitter channel to the context, allowing node
// functions and tool handlers to emit custom events during execution.
func WithEmitter(ctx context.Context, ch chan<- Event) context.Context {
	return context.WithValue(ctx, emitKey, ch)
}

// Emit sends a custom event from within a node function or tool handler.
// It is a no-op if no emitter is attached to the context.
func Emit(ctx context.Context, eventType string, data any) {
	ch, ok := ctx.Value(emitKey).(chan<- Event)
	if !ok || ch == nil {
		return
	}
	select {
	case ch <- Event{Type: EventCustom, Data: map[string]any{
		"custom_type": eventType,
		"payload":     data,
	}}:
	default:
	}
}
