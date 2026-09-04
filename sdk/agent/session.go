package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage"
)

// ChatSession manages multi-turn conversation state with automatic
// context-window summarization.
type ChatSession struct {
	ID       string          `json:"id"`
	AgentID  string          `json:"agent_id"`
	Messages []model.Message `json:"messages"`
	Summary  string          `json:"summary"`

	mu sync.Mutex
}

// chatSessionFromEvents reconstructs a ChatSession from the event ledger.
func chatSessionFromEvents(events []*storage.Event) *ChatSession {
	cs := &ChatSession{}
	for _, evt := range events {
		payload, ok := evt.Payload.(map[string]any)
		if !ok {
			continue
		}
		switch evt.Type {
		case "chat_message":
			role, _ := payload["role"].(string)
			content, _ := payload["content"].(string)
			msg := model.Message{Role: role, Content: content}
			if name, ok := payload["name"].(string); ok {
				msg.Name = name
			}
			if tcID, ok := payload["tool_call_id"].(string); ok {
				msg.ToolCallID = tcID
			}
			if tcs, ok := payload["tool_calls"].([]any); ok {
				for _, raw := range tcs {
					if tcMap, ok := raw.(map[string]any); ok {
						msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{
							ID:        strFromMap(tcMap, "id"),
							Name:      strFromMap(tcMap, "name"),
							Arguments: strFromMap(tcMap, "arguments"),
						})
					}
				}
			}
			cs.Messages = append(cs.Messages, msg)
		case "chat_summary":
			if s, ok := payload["summary"].(string); ok {
				cs.Summary = s
			}
		}
	}
	return cs
}

func strFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// persistMessage appends a single chat message event to the storage ledger.
func persistMessage(ctx context.Context, store storage.Storage, sessionID string, seqNum int64, msg model.Message) error {
	payload := map[string]any{
		"role":    msg.Role,
		"content": msg.Content,
	}
	if msg.Name != "" {
		payload["name"] = msg.Name
	}
	if msg.ToolCallID != "" {
		payload["tool_call_id"] = msg.ToolCallID
	}
	if len(msg.ToolCalls) > 0 {
		tcs := make([]map[string]any, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			tcs[i] = map[string]any{"id": tc.ID, "name": tc.Name, "arguments": tc.Arguments}
		}
		payload["tool_calls"] = tcs
	}

	return store.AppendEvent(ctx, &storage.Event{
		ID:        fmt.Sprintf("chat_%s_%d", sessionID, seqNum),
		SessionID: sessionID,
		SeqNum:    seqNum,
		Type:      "chat_message",
		Payload:   payload,
		CreatedAt: time.Now(),
	})
}

// persistSummary stores a summarization event in the ledger.
func persistSummary(ctx context.Context, store storage.Storage, sessionID string, seqNum int64, summary string) error {
	return store.AppendEvent(ctx, &storage.Event{
		ID:        fmt.Sprintf("summary_%s_%d", sessionID, seqNum),
		SessionID: sessionID,
		SeqNum:    seqNum,
		Type:      "chat_summary",
		Payload:   map[string]any{"summary": summary},
		CreatedAt: time.Now(),
	})
}

// CompactSession forces a summarization pass over sessionID's history right
// now, regardless of how close the conversation is to the model's context
// window — unlike the automatic compaction ChatWithSession performs inline,
// which only triggers once NeedsSummarization crosses SummarizeThreshold.
// This lets a caller recovering from an out-of-band failure that has nothing
// to do with context size (e.g. a cost/budget cap) shrink the session's
// history and keep using the same session instead of discarding the
// conversation outright. It is a no-op if the session has no messages.
func (a *Agent) CompactSession(ctx context.Context, sessionID string) error {
	if a.Model == nil {
		return fmt.Errorf("agent %q has no model", a.ID)
	}
	if a.Storage == nil {
		return fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}

	events, err := a.Storage.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("load session events: %w", err)
	}
	cs := chatSessionFromEvents(events)
	if len(cs.Messages) == 0 {
		return nil
	}

	counter := model.NewTokenCounter(a.Model.Model())
	summarizer := model.NewSummarizer(a.Model, counter, model.SummarizationConfig{
		Threshold:           a.ContextCfg.SummarizeThreshold,
		PreserveRecentTurns: a.ContextCfg.PreserveRecentTurns,
	})
	result, sumErr := summarizer.Summarize(ctx, cs.Summary, cs.Messages)
	if sumErr != nil {
		return fmt.Errorf("summarize: %w", sumErr)
	}

	seqNum := int64(len(events) + 1)
	if persistErr := persistSummary(ctx, a.Storage, sessionID, seqNum, result.Summary); persistErr != nil {
		return fmt.Errorf("persist summary: %w", persistErr)
	}

	_ = a.Hooks.After(ctx, &hooks.Event{
		Type: hooks.EventSummarization,
		Name: sessionID,
		Metadata: map[string]any{
			"summary_length":     len(result.Summary),
			"preserved_messages": len(result.PreservedMessages),
			"forced":             true,
		},
	})
	return nil
}

// ChatWithSession sends a message within a persistent, multi-turn session.
// When the conversation approaches the model's context window limit, older
// messages are automatically summarized to stay within budget.
func (a *Agent) ChatWithSession(ctx context.Context, sessionID, userMessage string) (*model.ChatResponse, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("agent %q has no model", a.ID)
	}
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}

	// Scope per-session harness state (planning tool, VFS) to this session so
	// tools that persist across turns resolve the right session from context.
	ctx = storage.WithSession(ctx, sessionID)

	// Fire session start hook on first call (best-effort, idempotent)
	_ = a.Hooks.Before(ctx, &hooks.Event{Type: hooks.EventSessionStart, Name: sessionID})

	// Ensure the session exists in storage
	if _, err := a.Storage.GetSession(ctx, sessionID); err != nil {
		sess := &storage.Session{
			ID:        sessionID,
			AgentID:   a.ID,
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if createErr := a.Storage.CreateSession(ctx, sess); createErr != nil {
			return nil, fmt.Errorf("create session: %w", createErr)
		}
	}

	// Reconstruct session from event ledger
	events, err := a.Storage.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	cs := chatSessionFromEvents(events)
	cs.ID = sessionID
	cs.AgentID = a.ID

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Append user message
	userMsg := model.Message{Role: model.RoleUser, Content: userMessage}
	cs.Messages = append(cs.Messages, userMsg)
	seqNum := int64(len(events) + 1)
	if persistErr := persistMessage(ctx, a.Storage, sessionID, seqNum, userMsg); persistErr != nil {
		return nil, fmt.Errorf("persist user message: %w", persistErr)
	}

	// Build the system context (prompt, instructions, memories, knowledge)
	systemMsgs := a.buildSystemContext(ctx, userMessage)

	// Resolve context limit. Use the real BPE tokenizer (WC-A-004 / PLAN.md
	// P1-009) so the compaction trigger and budget reflect actual token counts,
	// not the 4-chars-per-token heuristic.
	counter := model.NewTokenCounter(a.Model.Model())
	contextLimit := a.resolveContextLimit()
	systemTokens := counter.CountTokens(systemMsgs)

	// Check if summarization is needed
	summarizer := model.NewSummarizer(a.Model, counter, model.SummarizationConfig{
		Threshold:           a.ContextCfg.SummarizeThreshold,
		PreserveRecentTurns: a.ContextCfg.PreserveRecentTurns,
	})

	if summarizer.NeedsSummarization(systemTokens, cs.Messages, contextLimit) {
		_ = a.Hooks.Before(ctx, &hooks.Event{
			Type: hooks.EventContextOverflow,
			Name: sessionID,
			Metadata: map[string]any{
				"estimated_tokens": systemTokens + counter.CountTokens(cs.Messages),
				"context_limit":    contextLimit,
			},
		})

		result, sumErr := summarizer.Summarize(ctx, cs.Summary, cs.Messages)
		if sumErr != nil {
			return nil, fmt.Errorf("summarize: %w", sumErr)
		}

		cs.Summary = result.Summary
		cs.Messages = result.PreservedMessages

		seqNum++
		if sumPersistErr := persistSummary(ctx, a.Storage, sessionID, seqNum, cs.Summary); sumPersistErr != nil {
			return nil, fmt.Errorf("persist summary: %w", sumPersistErr)
		}

		_ = a.Hooks.After(ctx, &hooks.Event{
			Type: hooks.EventSummarization,
			Name: sessionID,
			Metadata: map[string]any{
				"summary_length":     len(cs.Summary),
				"preserved_messages": len(cs.Messages),
			},
		})
	}

	// Build final message array
	messages := make([]model.Message, 0, len(systemMsgs)+len(cs.Messages)+1)
	messages = append(messages, systemMsgs...)
	protectedPrefix := len(systemMsgs)
	if cs.Summary != "" {
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Previous conversation summary:\n" + cs.Summary,
		})
		protectedPrefix++
	}
	messages = append(messages, cs.Messages...)

	// Final budget safeguard: summarization bounds *growth* (it drops old turns),
	// but the preserved recent turns are kept verbatim and uncapped, so a few very
	// large recent turns could still overflow. Trim the oldest conversation turns —
	// never the pinned/system prefix or the summary — until the request fits, so
	// the in-flight token count stays bounded. Only the request sent to the model
	// is trimmed; the full history remains in the ledger (cs.Messages). If the
	// protected prefix alone exceeds the window (e.g. oversized pins) nothing more
	// can be dropped — keep pins compact.
	messages = enforceContextBudget(counter, messages, protectedPrefix, contextLimit)

	// Check input guardrails
	if result := a.Guardrails.CheckInput(ctx, userMessage); result != nil {
		return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
	}

	req := &model.ChatRequest{Messages: messages}
	applyOutputSchema(req, a.OutputSchema)

	// Add tool definitions
	tools := a.Tools.List()
	if len(tools) > 0 {
		for _, t := range tools {
			req.Tools = append(req.Tools, model.ToolDefinition{
				Type: "function",
				Function: model.FunctionDef{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			})
		}
	}

	// Fire model call hooks, passing provider and request for retry hook
	modelEvt := &hooks.Event{
		Type:  hooks.EventModelCallBefore,
		Name:  a.Model.Name(),
		Input: req,
		Metadata: map[string]any{
			"provider": a.Model,
			"request":  req,
		},
	}
	if hookErr := a.Hooks.Before(ctx, modelEvt); hookErr != nil {
		return nil, fmt.Errorf("hook before model call: %w", hookErr)
	}

	// The session id is in ctx (set above), so these route to the session topic:
	// the AG-UI/native per-session stream works for ChatWithSession too.
	a.publish(ctx, stream.Event{Type: stream.EventModelCall, Data: map[string]any{
		"agent": a.ID, "model": a.Model.Name(), "messages": len(req.Messages),
	}})

	resp, err := a.Model.Chat(ctx, req)

	modelEvt.Type = hooks.EventModelCallAfter
	modelEvt.Output = resp
	modelEvt.Error = err
	_ = a.Hooks.After(ctx, modelEvt)

	// If retry hook succeeded, use its output
	if err != nil && modelEvt.Error == nil {
		resp, _ = modelEvt.Output.(*model.ChatResponse)
		err = nil
	}

	if err != nil {
		a.publish(ctx, stream.Event{Type: stream.EventError, Data: map[string]any{
			"agent": a.ID, "error": err.Error(),
		}})
		return nil, fmt.Errorf("agent %q session chat: %w", a.ID, err)
	}

	a.publish(ctx, stream.Event{Type: stream.EventModelResponse, Data: map[string]any{
		"agent": a.ID, "stop_reason": string(resp.StopReason), "content": resp.Content,
	}})

	// Handle tool calls across multiple rounds, threading the accumulated
	// message history and passing the tool definitions on every follow-up.
	maxIter := a.MaxIterations
	if maxIter <= 0 {
		maxIter = 25
	}
	iteration := 0
	for resp.StopReason == model.StopReasonToolCall && len(resp.ToolCalls) > 0 {
		iteration++
		if iteration > maxIter {
			return nil, fmt.Errorf("agent %q: exceeded max tool-calling iterations (%d) with unsatisfied tool calls", a.ID, maxIter)
		}
		resp, messages, err = a.handleToolCalls(ctx, messages, resp, req)
		if err != nil {
			return nil, err
		}
	}

	// Check output guardrails
	if resp != nil && resp.Content != "" {
		if result := a.Guardrails.CheckOutput(ctx, resp.Content); result != nil {
			return nil, fmt.Errorf("output guardrail failed: %s", result.Reason)
		}
	}

	// Validate response against output schema
	if a.OutputSchema != nil && resp != nil && resp.Content != "" {
		if valErr := validateAgainstSchema(resp.Content, a.OutputSchema); valErr != nil {
			return nil, fmt.Errorf("output schema validation failed: %w", valErr)
		}
	}

	// Persist assistant response
	if resp != nil {
		assistantMsg := model.Message{Role: model.RoleAssistant, Content: resp.Content}
		cs.Messages = append(cs.Messages, assistantMsg)
		seqNum++
		if pErr := persistMessage(ctx, a.Storage, sessionID, seqNum, assistantMsg); pErr != nil {
			return nil, fmt.Errorf("persist assistant message: %w", pErr)
		}
	}

	// Extract memories (scoped to the agent's tenant)
	if mgr := a.memoryManager(); mgr != nil {
		_ = mgr.ExtractMemories(ctx, cs.Messages)
	}

	return resp, nil
}

// ChatStreamWithSession streams a message within a persistent, multi-turn
// session. It uses the same event-ledger history and context budgeting as
// ChatWithSession, and persists the completed assistant response before the
// stream closes.
func (a *Agent) ChatStreamWithSession(ctx context.Context, sessionID, userMessage string) (<-chan *model.ChatResponse, error) {
	if a.Model == nil {
		return nil, fmt.Errorf("agent %q has no model", a.ID)
	}
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}

	ctx = storage.WithSession(ctx, sessionID)
	_ = a.Hooks.Before(ctx, &hooks.Event{Type: hooks.EventSessionStart, Name: sessionID})

	if _, err := a.Storage.GetSession(ctx, sessionID); err != nil {
		sess := &storage.Session{
			ID:        sessionID,
			AgentID:   a.ID,
			Status:    "active",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if createErr := a.Storage.CreateSession(ctx, sess); createErr != nil {
			return nil, fmt.Errorf("create session: %w", createErr)
		}
	}

	events, err := a.Storage.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("load session events: %w", err)
	}
	cs := chatSessionFromEvents(events)
	cs.ID = sessionID
	cs.AgentID = a.ID
	cs.mu.Lock()

	userMsg := model.Message{Role: model.RoleUser, Content: userMessage}
	cs.Messages = append(cs.Messages, userMsg)
	seqNum := int64(len(events) + 1)
	if err := persistMessage(ctx, a.Storage, sessionID, seqNum, userMsg); err != nil {
		cs.mu.Unlock()
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	systemMsgs := a.buildSystemContext(ctx, userMessage)
	counter := model.NewTokenCounter(a.Model.Model())
	contextLimit := a.resolveContextLimit()
	systemTokens := counter.CountTokens(systemMsgs)
	summarizer := model.NewSummarizer(a.Model, counter, model.SummarizationConfig{
		Threshold:           a.ContextCfg.SummarizeThreshold,
		PreserveRecentTurns: a.ContextCfg.PreserveRecentTurns,
	})
	if summarizer.NeedsSummarization(systemTokens, cs.Messages, contextLimit) {
		_ = a.Hooks.Before(ctx, &hooks.Event{
			Type: hooks.EventContextOverflow,
			Name: sessionID,
			Metadata: map[string]any{
				"estimated_tokens": systemTokens + counter.CountTokens(cs.Messages),
				"context_limit":    contextLimit,
			},
		})
		result, sumErr := summarizer.Summarize(ctx, cs.Summary, cs.Messages)
		if sumErr != nil {
			cs.mu.Unlock()
			return nil, fmt.Errorf("summarize: %w", sumErr)
		}
		cs.Summary = result.Summary
		cs.Messages = result.PreservedMessages
		seqNum++
		if err := persistSummary(ctx, a.Storage, sessionID, seqNum, cs.Summary); err != nil {
			cs.mu.Unlock()
			return nil, fmt.Errorf("persist summary: %w", err)
		}
		_ = a.Hooks.After(ctx, &hooks.Event{
			Type: hooks.EventSummarization,
			Name: sessionID,
			Metadata: map[string]any{
				"summary_length":     len(cs.Summary),
				"preserved_messages": len(cs.Messages),
			},
		})
	}

	messages := make([]model.Message, 0, len(systemMsgs)+len(cs.Messages)+1)
	messages = append(messages, systemMsgs...)
	protectedPrefix := len(systemMsgs)
	if cs.Summary != "" {
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Previous conversation summary:\n" + cs.Summary,
		})
		protectedPrefix++
	}
	messages = append(messages, cs.Messages...)
	messages = enforceContextBudget(counter, messages, protectedPrefix, contextLimit)

	if result := a.Guardrails.CheckInput(ctx, userMessage); result != nil {
		cs.mu.Unlock()
		return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
	}

	req := &model.ChatRequest{Messages: messages}
	applyOutputSchema(req, a.OutputSchema)
	for _, t := range a.Tools.List() {
		req.Tools = append(req.Tools, model.ToolDefinition{
			Type: "function",
			Function: model.FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}

	out := make(chan *model.ChatResponse, 64)
	go func() {
		defer close(out)
		defer cs.mu.Unlock()
		resp, _, streamErr := a.streamLoop(ctx, req, messages, out)
		if streamErr != nil || resp == nil {
			return
		}
		assistantMsg := model.Message{Role: model.RoleAssistant, Content: resp.Content}
		if err := persistMessage(ctx, a.Storage, sessionID, seqNum+1, assistantMsg); err != nil {
			a.emitError(ctx, out, fmt.Errorf("persist assistant message: %w", err))
		}
	}()
	return out, nil
}

// buildSystemContext constructs the system-level messages (prompt, instructions,
// memories, knowledge) without the conversation history.
func (a *Agent) buildSystemContext(ctx context.Context, userQuery string) []model.Message {
	messages := make([]model.Message, 0, 8)
	if a.SystemPrompt != "" {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: a.SystemPrompt})
	}
	for _, inst := range a.Instructions {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: inst})
	}
	// Pinned context (static + dynamic, e.g. the active plan) is part of the
	// system context, so it is counted in the budget and — because compaction
	// only summarizes conversation turns, never systemMsgs — always retained.
	messages = append(messages, a.pinnedMessages(ctx)...)
	messages = append(messages, a.memoryMessages(ctx, userQuery)...)
	if a.Knowledge != nil {
		if docs, err := a.Knowledge.Search(ctx, userQuery, 5); err == nil && len(docs) > 0 {
			var kb strings.Builder
			kb.WriteString("Relevant knowledge:\n")
			for _, d := range docs {
				kb.WriteString("- ")
				kb.WriteString(d.Content)
				kb.WriteString("\n")
			}
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: kb.String()})
		}
	}
	return messages
}

// resolveContextLimit determines the effective context window size for the model.
func (a *Agent) resolveContextLimit() int {
	if a.ContextCfg.MaxContextTokens > 0 {
		return a.ContextCfg.MaxContextTokens
	}
	return model.ContextLimit(a.Model.Model(), 0)
}
