package agent

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
)

// sessionRecorder owns the ledger sequence and conversation state of one
// session chat call. It persists the final assistant message and, with
// ContextConfig.PersistToolRounds, every completed tool round and in-turn
// compaction checkpoint. The caller holds the session lock throughout.
type sessionRecorder struct {
	agent     *Agent
	sessionID string
	cs        *ChatSession
	// seq is the last ledger sequence written by this call.
	seq int64
	// turnStart indexes the current user message in cs.Messages.
	turnStart int
	persist   bool

	systemMsgs   []model.Message
	systemTokens int
	counter      model.TokenCounter
	contextLimit int
	summarizer   *model.Summarizer
	// compactedLen is len(cs.Messages) after the last in-turn compaction.
	compactedLen int
}

// newSessionRecorder starts recording after the current user message, which
// is the last element of cs.Messages, and any turn-start summary; seq is the
// last sequence already written.
func (a *Agent) newSessionRecorder(sessionID string, cs *ChatSession, seq int64, systemMsgs []model.Message, systemTokens int, counter model.TokenCounter, contextLimit int, summarizer *model.Summarizer) *sessionRecorder {
	return &sessionRecorder{
		agent: a, sessionID: sessionID, cs: cs, seq: seq,
		turnStart: len(cs.Messages) - 1, persist: a.ContextCfg.PersistToolRounds,
		systemMsgs: systemMsgs, systemTokens: systemTokens, counter: counter,
		contextLimit: contextLimit, summarizer: summarizer, compactedLen: len(cs.Messages),
	}
}

// minCompactionGrowth avoids a summarizer call on every round when the
// preserved tail alone stays above the compaction threshold.
const minCompactionGrowth = 4

// recordRound appends a completed round to the conversation and ledger.
func (r *sessionRecorder) recordRound(ctx context.Context, round []model.Message) error {
	if !r.persist {
		return nil
	}
	for _, msg := range round {
		if err := r.append(ctx, msg); err != nil {
			return fmt.Errorf("persist tool round: %w", err)
		}
	}
	return nil
}

// finish persists the final assistant message of the call.
func (r *sessionRecorder) finish(ctx context.Context, msg model.Message) error {
	return r.append(ctx, msg)
}

func (r *sessionRecorder) append(ctx context.Context, msg model.Message) error {
	r.cs.Messages = append(r.cs.Messages, msg)
	r.seq++
	return persistMessage(ctx, r.agent.Storage, r.sessionID, r.seq, msg)
}

// compactIfNeeded summarizes older conversation, including earlier rounds of
// the current turn, once the session nears the context window. The current
// user message is always preserved verbatim. It returns the rebuilt request
// messages, or nil when nothing changed. A failed summary is not fatal: the
// request-level budget still trims, so the turn continues uncompacted.
func (r *sessionRecorder) compactIfNeeded(ctx context.Context) ([]model.Message, error) {
	if !r.persist || r.summarizer == nil || len(r.cs.Messages) < r.compactedLen+minCompactionGrowth {
		return nil, nil
	}
	if !r.summarizer.NeedsSummarization(r.systemTokens, r.cs.Messages, r.contextLimit) {
		return nil, nil
	}
	task := r.cs.Messages[r.turnStart]
	prior := r.cs.Messages[:r.turnStart]
	rounds := r.cs.Messages[r.turnStart+1:]
	combined := make([]model.Message, 0, len(prior)+len(rounds))
	combined = append(combined, prior...)
	combined = append(combined, rounds...)

	result, err := r.summarizer.Summarize(ctx, r.cs.Summary, combined)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		r.compactedLen = len(r.cs.Messages)
		return nil, nil
	}
	if result.SummarizedCount == 0 {
		r.compactedLen = len(r.cs.Messages)
		return nil, nil
	}
	preserved := make([]model.Message, 0, len(result.PreservedMessages)+1)
	turnStart := 0
	if priorKept := len(result.PreservedMessages) - len(rounds); priorKept > 0 {
		preserved = append(preserved, result.PreservedMessages[:priorKept]...)
		turnStart = priorKept
		preserved = append(preserved, task)
		preserved = append(preserved, result.PreservedMessages[priorKept:]...)
	} else {
		preserved = append(preserved, task)
		preserved = append(preserved, result.PreservedMessages...)
	}
	checkpoint := model.SummarizationResult{Summary: result.Summary, PreservedMessages: preserved, SummarizedCount: result.SummarizedCount}
	r.seq++
	if err := persistSummaryCheckpoint(ctx, r.agent.Storage, r.sessionID, r.seq, r.seq-1, checkpoint); err != nil {
		return nil, fmt.Errorf("persist in-turn summary: %w", err)
	}
	r.cs.Summary = result.Summary
	r.cs.Messages = preserved
	r.turnStart = turnStart
	r.compactedLen = len(preserved)
	_ = r.agent.Hooks.After(ctx, &hooks.Event{
		Type: hooks.EventSummarization,
		Name: r.sessionID,
		Metadata: map[string]any{
			"summary_length":     len(r.cs.Summary),
			"preserved_messages": len(r.cs.Messages),
			"in_turn":            true,
		},
	})
	return r.requestMessages(), nil
}

// requestMessages rebuilds the model request from system context, the running
// summary, and the conversation, trimmed to the context window.
func (r *sessionRecorder) requestMessages() []model.Message {
	messages := make([]model.Message, 0, len(r.systemMsgs)+len(r.cs.Messages)+1)
	messages = append(messages, r.systemMsgs...)
	protectedPrefix := len(r.systemMsgs)
	if r.cs.Summary != "" {
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Previous conversation summary:\n" + r.cs.Summary,
		})
		protectedPrefix++
	}
	messages = append(messages, r.cs.Messages...)
	return enforceContextBudget(r.counter, messages, protectedPrefix, r.contextLimit)
}

// repairToolPairs keeps only complete tool rounds: an assistant message with
// tool calls followed by a result for every call. A crash between persisting
// a round's messages, or an older summary split, must not leave a dangling
// call or orphaned result that providers reject. Calls without all results
// keep their text but lose the calls and the provider state bound to them.
func repairToolPairs(messages []model.Message) []model.Message {
	out := make([]model.Message, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		switch {
		case msg.Role == model.RoleTool:
			continue
		case msg.Role == model.RoleAssistant && len(msg.ToolCalls) > 0:
			end := i + 1
			results := make(map[string]struct{})
			for end < len(messages) && messages[end].Role == model.RoleTool {
				results[messages[end].ToolCallID] = struct{}{}
				end++
			}
			complete := true
			for _, call := range msg.ToolCalls {
				if _, ok := results[call.ID]; !ok {
					complete = false
					break
				}
			}
			if complete {
				out = append(out, messages[i:end]...)
			} else if msg.Content != "" {
				out = append(out, model.Message{Role: msg.Role, Content: msg.Content, Name: msg.Name})
			}
			i = end - 1
		default:
			out = append(out, msg)
		}
	}
	return out
}
