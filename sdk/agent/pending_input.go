package agent

import (
	"context"
	"strings"

	"github.com/spawn08/chronos/engine/model"
)

// PendingInput supplies user messages submitted while a chat call is still
// running, so an interactive client can add context to the task in progress
// instead of interrupting it. The loop drains it at each tool-round boundary,
// after every call of the round has its result and only when the loop
// continues, so drained input is always answered by the next model call.
// Input left undrained when the call ends stays with the source; the client
// decides whether it becomes the next turn.
type PendingInput interface {
	// DrainPendingInput returns and removes every message submitted since the
	// previous drain, oldest first.
	DrainPendingInput(ctx context.Context) []string
}

type pendingInputKey struct{}

type boundPendingInput struct {
	agentID string
	source  PendingInput
}

// WithPendingInput installs source for agentID's tool loops in ctx. Like
// WithToolLoopController the binding is per agent, so subagents and team
// members that inherit ctx never consume input meant for the caller.
func WithPendingInput(ctx context.Context, agentID string, source PendingInput) context.Context {
	if source == nil {
		return ctx
	}
	return context.WithValue(ctx, pendingInputKey{}, boundPendingInput{agentID: agentID, source: source})
}

func pendingInputFor(ctx context.Context, agentID string) PendingInput {
	bound, ok := ctx.Value(pendingInputKey{}).(boundPendingInput)
	if !ok || bound.agentID != agentID {
		return nil
	}
	return bound.source
}

// pendingUserMessage drains source into one user message; ok is false when
// nothing non-blank is pending. Messages are joined so the conversation gains
// a single user turn after the round's tool results.
func pendingUserMessage(ctx context.Context, source PendingInput) (model.Message, bool) {
	if source == nil {
		return model.Message{}, false
	}
	parts := make([]string, 0, 1)
	for _, text := range source.DrainPendingInput(ctx) {
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	if len(parts) == 0 {
		return model.Message{}, false
	}
	return model.Message{Role: model.RoleUser, Content: strings.Join(parts, "\n\n")}, true
}
