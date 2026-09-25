package agent

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
)

// ToolRound describes one completed tool-calling round: the model requested
// ToolCalls and every call has produced a result.
type ToolRound struct {
	AgentID string
	// Iteration is the 1-based round number within the current chat call.
	Iteration int
	ToolCalls []model.ToolCall
	// Usage is the usage of the model call that requested ToolCalls.
	Usage model.Usage
}

// ToolLoopAction tells the agent how to proceed after a tool round.
type ToolLoopAction struct {
	// Stop ends the loop before the next model call. The call returns a
	// response with StopReasonPaused and Message as its content.
	Stop    bool
	Message string
}

// ToolLoopController replaces the fixed MaxIterations cap for one chat call.
// It is consulted at each round boundary, after tool results are appended
// (and persisted, for session chat) and before the follow-up model call, so a
// Stop never leaves a tool call without its result. Returning an error fails
// the call with that error.
type ToolLoopController interface {
	AfterToolRound(ctx context.Context, round ToolRound) (ToolLoopAction, error)
}

type toolLoopControllerKey struct{}

type boundToolLoopController struct {
	agentID    string
	controller ToolLoopController
}

// WithToolLoopController installs controller for agentID's tool loops in
// ctx. The binding is per agent so nested calls by other agents (subagents,
// team members) that inherit ctx keep their own policy, or the fixed cap.
// Installing another controller for any agent replaces the binding.
func WithToolLoopController(ctx context.Context, agentID string, controller ToolLoopController) context.Context {
	if controller == nil {
		return ctx
	}
	return context.WithValue(ctx, toolLoopControllerKey{}, boundToolLoopController{agentID: agentID, controller: controller})
}

func toolLoopControllerFor(ctx context.Context, agentID string) ToolLoopController {
	bound, ok := ctx.Value(toolLoopControllerKey{}).(boundToolLoopController)
	if !ok || bound.agentID != agentID {
		return nil
	}
	return bound.controller
}

// toolLoop threads loop control and optional session persistence through
// every tool-calling loop (blocking, streaming, with or without a session).
type toolLoop struct {
	agent      *Agent
	controller ToolLoopController
	limit      int
	iteration  int
	session    *sessionRecorder
}

func (a *Agent) newToolLoop(ctx context.Context, session *sessionRecorder) *toolLoop {
	return &toolLoop{agent: a, controller: toolLoopControllerFor(ctx, a.ID), limit: a.toolLoopLimit(), session: session}
}

// beforeRound enforces the fixed iteration cap unless a controller owns
// bounding for this call.
func (l *toolLoop) beforeRound() error {
	l.iteration++
	if l.controller == nil && l.iteration > l.limit {
		return fmt.Errorf("agent %q: exceeded max tool-calling iterations (%d) with unsatisfied tool calls", l.agent.ID, l.limit)
	}
	return nil
}

// afterRound records the executed round (messages[before:]), consults the
// controller, then compacts the session when it nears the context window.
// It returns the working messages to send next, which differ from messages
// only after compaction, and a non-nil action when the loop must stop.
func (l *toolLoop) afterRound(ctx context.Context, messages []model.Message, before int, resp *model.ChatResponse) ([]model.Message, *ToolLoopAction, error) {
	if l.session != nil {
		if err := l.session.recordRound(ctx, messages[before:]); err != nil {
			return messages, nil, err
		}
	}
	if l.controller != nil {
		action, err := l.controller.AfterToolRound(ctx, ToolRound{
			AgentID:   l.agent.ID,
			Iteration: l.iteration,
			ToolCalls: append([]model.ToolCall(nil), resp.ToolCalls...),
			Usage:     resp.Usage,
		})
		if err != nil {
			return messages, nil, fmt.Errorf("agent %q tool loop: %w", l.agent.ID, err)
		}
		if action.Stop {
			return messages, &action, nil
		}
	}
	if l.session != nil {
		rebuilt, err := l.session.compactIfNeeded(ctx)
		if err != nil {
			return messages, nil, err
		}
		if rebuilt != nil {
			return rebuilt, nil, nil
		}
	}
	return messages, nil, nil
}

func pausedResponse(action *ToolLoopAction) *model.ChatResponse {
	return &model.ChatResponse{Role: model.RoleAssistant, Content: action.Message, StopReason: model.StopReasonPaused}
}
