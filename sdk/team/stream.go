package team

import (
	"context"
	"strings"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

// TeamEventType classifies a TeamStreamEvent.
type TeamEventType string

const (
	// TeamEventAgentStart is emitted just before an agent begins producing output.
	TeamEventAgentStart TeamEventType = "agent_start"
	// TeamEventToken carries a single text fragment from an agent as it streams.
	TeamEventToken TeamEventType = "token"
	// TeamEventAgentEnd is emitted after an agent finishes producing output.
	TeamEventAgentEnd TeamEventType = "agent_end"
	// TeamEventComplete is the final event carrying the team's merged output state.
	TeamEventComplete TeamEventType = "complete"
	// TeamEventError is a terminal event carrying a fatal error.
	TeamEventError TeamEventType = "error"
)

// TeamStreamEvent is a single event emitted while a team runs in streaming mode.
// Events from different agents may interleave (e.g. under the parallel strategy);
// use AgentID to attribute each token to its producer.
type TeamStreamEvent struct {
	Type    TeamEventType `json:"type"`
	AgentID string        `json:"agent_id,omitempty"`
	Content string        `json:"content,omitempty"` // token text for TeamEventToken
	State   graph.State   `json:"state,omitempty"`   // final state for TeamEventComplete
	Err     error         `json:"-"`                 // set for TeamEventError
}

// sinkKey is the context key under which a team stream sink is stored.
type sinkKey struct{}

// withSink attaches an event sink to ctx so agent execution deep in a strategy
// can forward tokens without threading a channel through every signature.
func withSink(ctx context.Context, fn func(TeamStreamEvent)) context.Context {
	return context.WithValue(ctx, sinkKey{}, fn)
}

// sinkFrom returns the event sink attached to ctx, if any. A present sink is the
// signal that the team is running in streaming mode; when absent, agent execution
// stays on the blocking path so the same code serves both callers.
func sinkFrom(ctx context.Context) (func(TeamStreamEvent), bool) {
	fn, ok := ctx.Value(sinkKey{}).(func(TeamStreamEvent))
	return fn, ok
}

// streamAgentContent runs an agent on msg and returns its full response. When a
// stream sink is attached to ctx (i.e. the team was started via RunStream) and
// the agent has a model, it streams tokens through ChatStream — emitting
// agent_start, token and agent_end events — while still aggregating the complete
// response so the calling strategy's control flow is unchanged. Without a sink,
// it falls back to the blocking Chat, preserving the non-streaming behavior
// exactly.
func streamAgentContent(ctx context.Context, a *agent.Agent, msg string) (*model.ChatResponse, error) {
	sink, streaming := sinkFrom(ctx)
	if !streaming || a.Model == nil {
		return a.Chat(ctx, msg)
	}

	sink(TeamStreamEvent{Type: TeamEventAgentStart, AgentID: a.ID})
	defer sink(TeamStreamEvent{Type: TeamEventAgentEnd, AgentID: a.ID})

	ch, err := a.ChatStream(ctx, msg)
	if err != nil {
		return nil, err
	}

	resp := &model.ChatResponse{Role: model.RoleAssistant}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		if chunk.Delta {
			b.WriteString(chunk.Content)
			sink(TeamStreamEvent{Type: TeamEventToken, AgentID: a.ID, Content: chunk.Content})
			continue
		}
		resp.Usage = chunk.Usage
		resp.StopReason = chunk.StopReason
	}
	resp.Content = b.String()
	return resp, nil
}

// RunStream executes the team's strategy and streams agent output token by token.
// It returns a channel of TeamStreamEvent that the caller must drain to
// completion; the channel is closed when the run finishes.
//
// The stream always ends with exactly one terminal event: TeamEventComplete
// (carrying the merged final State) on success, or TeamEventError on failure.
//
// Streaming is supported for the sequential, parallel, router, coordinator and
// hierarchy strategies. The swarm strategy runs to completion but does not emit
// token events, because it inspects tool-call output to route handoffs.
func (t *Team) RunStream(ctx context.Context, input graph.State) (<-chan TeamStreamEvent, error) {
	out := make(chan TeamStreamEvent, 128)
	sink := func(evt TeamStreamEvent) {
		select {
		case out <- evt:
		case <-ctx.Done():
		}
	}

	streamCtx := withSink(ctx, sink)
	go func() {
		defer close(out)
		result, err := t.Run(streamCtx, input)
		if err != nil {
			sink(TeamStreamEvent{Type: TeamEventError, Err: err})
			return
		}
		sink(TeamStreamEvent{Type: TeamEventComplete, State: result})
	}()
	return out, nil
}
