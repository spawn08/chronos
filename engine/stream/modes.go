package stream

// StreamMode defines which events are included in a stream subscription.
type StreamMode string

const (
	// ModeValues streams full state after each node execution.
	ModeValues StreamMode = "values"
	// ModeUpdates streams only changed state keys after each node.
	ModeUpdates StreamMode = "updates"
	// ModeCustom streams only user-emitted custom events.
	ModeCustom StreamMode = "custom"
	// ModeMessages streams LLM token-level events (model_call, model_response).
	ModeMessages StreamMode = "messages"
	// ModeDebug streams all internal execution details.
	ModeDebug StreamMode = "debug"
)

// StreamConfig holds configuration for how events are filtered and delivered.
type StreamConfig struct {
	Mode StreamMode
}

// DefaultStreamConfig returns a config that streams all events (debug mode).
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{Mode: ModeDebug}
}

var modeFilters = map[StreamMode]map[string]bool{
	ModeValues: {
		EventNodeEnd:   true,
		EventCompleted: true,
	},
	ModeUpdates: {
		EventNodeStart: true,
		EventNodeEnd:   true,
		EventCompleted: true,
	},
	ModeCustom: {
		EventCustom: true,
	},
	ModeMessages: {
		EventModelCall:     true,
		EventModelResponse: true,
	},
}

// ShouldInclude returns true if the given event type passes the mode's filter.
// ModeDebug includes all events.
func (c StreamConfig) ShouldInclude(eventType string) bool {
	if c.Mode == ModeDebug {
		return true
	}
	filter, ok := modeFilters[c.Mode]
	if !ok {
		return true
	}
	return filter[eventType]
}
