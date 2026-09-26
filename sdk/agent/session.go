package agent

import (
	"context"
	"encoding/json"
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

	mu          sync.Mutex
	recoveryErr error
}

type sessionLock struct {
	gate chan struct{}
	refs int
}

func (a *Agent) acquireSession(ctx context.Context, sessionID string) (func(), error) {
	a.sessionLocksMu.Lock()
	if a.sessionLocks == nil {
		a.sessionLocks = make(map[string]*sessionLock)
	}
	lock := a.sessionLocks[sessionID]
	if lock == nil {
		lock = &sessionLock{gate: make(chan struct{}, 1)}
		a.sessionLocks[sessionID] = lock
	}
	lock.refs++
	a.sessionLocksMu.Unlock()

	select {
	case lock.gate <- struct{}{}:
		return func() {
			<-lock.gate
			a.releaseSessionRef(sessionID, lock)
		}, nil
	case <-ctx.Done():
		a.releaseSessionRef(sessionID, lock)
		return nil, ctx.Err()
	}
}

func (a *Agent) releaseSessionRef(sessionID string, lock *sessionLock) {
	a.sessionLocksMu.Lock()
	defer a.sessionLocksMu.Unlock()
	lock.refs--
	if lock.refs == 0 && a.sessionLocks[sessionID] == lock {
		delete(a.sessionLocks, sessionID)
	}
}

// chatSessionFromEvents reconstructs a ChatSession from the event ledger.
func chatSessionFromEvents(events []*storage.Event) *ChatSession {
	cs := &ChatSession{}
	var checkpointSeq, coveredSeq int64
	for _, evt := range events {
		if checkpoint, ok := decodeSummaryCheckpoint(evt); ok && evt.SeqNum > checkpointSeq {
			checkpointSeq = evt.SeqNum
			coveredSeq = checkpoint.CoveredSeq
			cs.Summary = checkpoint.Summary
			cs.Messages = checkpoint.PreservedMessages
		}
	}
	for _, evt := range events {
		payload, ok := evt.Payload.(map[string]any)
		if !ok {
			continue
		}
		switch evt.Type {
		case "chat_message":
			if checkpointSeq > 0 && evt.SeqNum <= coveredSeq {
				continue
			}
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
			} else if tcs, ok := payload["tool_calls"].([]map[string]any); ok {
				// Stores that keep payloads in memory return persistMessage's
				// concrete type rather than a JSON round-tripped []any.
				for _, tcMap := range tcs {
					msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{
						ID:        strFromMap(tcMap, "id"),
						Name:      strFromMap(tcMap, "name"),
						Arguments: strFromMap(tcMap, "arguments"),
					})
				}
			}
			if state, present := payload["provider_state"]; present {
				msg.ProviderState, cs.recoveryErr = decodeProviderState(state)
				if cs.recoveryErr != nil {
					return cs
				}
			}
			cs.Messages = append(cs.Messages, msg)
		case "chat_summary":
			if checkpointSeq > 0 && evt.SeqNum <= checkpointSeq {
				continue
			}
			if s, ok := payload["summary"].(string); ok {
				cs.Summary = s
			}
		}
	}
	return cs
}

// loadChatSession reconstructs the session for a new request. Unlike the
// faithful ledger replay, it keeps only complete tool rounds so a crash while
// a round was being persisted cannot produce a request providers reject.
func loadChatSession(events []*storage.Event) *ChatSession {
	cs := chatSessionFromEvents(events)
	cs.Messages = repairToolPairs(cs.Messages)
	return cs
}

// summaryCheckpoint replaces all chat messages through CoveredSeq with the
// exact preserved tail. CoveredSeq includes retained messages too: the snapshot
// supplies them once, while the historical ledger remains untouched.
type summaryCheckpoint struct {
	Version           int             `json:"version"`
	Summary           string          `json:"summary"`
	CoveredSeq        int64           `json:"covered_seq"`
	PreservedMessages []model.Message `json:"preserved_messages"`
}

func decodeSummaryCheckpoint(evt *storage.Event) (summaryCheckpoint, bool) {
	var checkpoint summaryCheckpoint
	if evt.Type != "chat_summary" {
		return checkpoint, false
	}
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		return checkpoint, false
	}
	// Missing/unknown/malformed checkpoint fields must never discard history.
	if _, ok := payload["summary"].(string); !ok {
		return checkpoint, false
	}
	if covered, ok := payload["covered_seq"]; !ok || covered == nil {
		return checkpoint, false
	}
	if _, ok := payload["preserved_messages"]; !ok {
		return checkpoint, false
	}
	data, err := json.Marshal(payload)
	if err != nil || json.Unmarshal(data, &checkpoint) != nil {
		return checkpoint, false
	}
	if value, exists := payload["provider_states"]; exists {
		encoded, err := json.Marshal(value)
		if err != nil {
			return checkpoint, false
		}
		var states []json.RawMessage
		if err := json.Unmarshal(encoded, &states); err != nil || len(states) != len(checkpoint.PreservedMessages) {
			return checkpoint, false
		}
		for i, state := range states {
			if string(state) == "null" {
				continue
			}
			checkpoint.PreservedMessages[i].ProviderState, err = decodeProviderState(state)
			if err != nil {
				return checkpoint, false
			}
		}
	}
	return checkpoint, checkpoint.Version == 1 && checkpoint.CoveredSeq >= 0 && checkpoint.CoveredSeq < evt.SeqNum
}

// nextSessionEventSequence includes non-chat events and gaps in the ledger.
// As with session writes generally, callers serialize writes to a session.
func nextSessionEventSequence(events []*storage.Event) int64 {
	var maxSeq int64
	for _, evt := range events {
		if evt.SeqNum > maxSeq {
			maxSeq = evt.SeqNum
		}
	}
	return maxSeq + 1
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
	state, err := encodeProviderState(msg.ProviderState)
	if err != nil {
		return fmt.Errorf("persist message provider continuation: %w", err)
	}
	if state != nil {
		payload["provider_state"] = state
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

// persistSummaryCheckpoint appends an additive v1 chat_summary event. coveredSeq
// is the last ledger sequence represented by result (including its preserved
// messages), not the number of messages summarized. seqNum must be newer.
func persistSummaryCheckpoint(ctx context.Context, store storage.Storage, sessionID string, seqNum, coveredSeq int64, result model.SummarizationResult) error {
	if coveredSeq < 0 || coveredSeq >= seqNum {
		return fmt.Errorf("invalid summary checkpoint sequence %d covering %d", seqNum, coveredSeq)
	}
	states := make([]*storedProviderState, len(result.PreservedMessages))
	for i, msg := range result.PreservedMessages {
		state, err := encodeProviderState(msg.ProviderState)
		if err != nil {
			return fmt.Errorf("persist summary provider continuation: %w", err)
		}
		states[i] = state
	}
	return store.AppendEvent(ctx, &storage.Event{
		ID:        fmt.Sprintf("summary_%s_%d", sessionID, seqNum),
		SessionID: sessionID,
		SeqNum:    seqNum,
		Type:      "chat_summary",
		Payload: map[string]any{
			"version":            1,
			"summary":            result.Summary,
			"covered_seq":        coveredSeq,
			"preserved_messages": result.PreservedMessages,
			"provider_states":    states,
		},
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
	provider := a.modelProvider(ctx)
	if provider == nil {
		return fmt.Errorf("agent %q has no model", a.ID)
	}
	ctx = WithModelProvider(ctx, provider)
	if a.Storage == nil {
		return fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}
	release, err := a.acquireSession(ctx, sessionID)
	if err != nil {
		return err
	}
	defer release()

	events, err := a.Storage.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("load session events: %w", err)
	}
	cs := loadChatSession(events)
	if cs.recoveryErr != nil {
		return fmt.Errorf("restore provider continuation: %w", cs.recoveryErr)
	}
	if len(cs.Messages) == 0 {
		return nil
	}

	counter := model.NewTokenCounter(provider.Model())
	summarizer := model.NewSummarizer(summarizerProvider{agent: a, provider: provider}, counter, model.SummarizationConfig{
		Threshold:           a.ContextCfg.SummarizeThreshold,
		PreserveRecentTurns: a.ContextCfg.PreserveRecentTurns,
	})
	result, sumErr := summarizer.Summarize(ctx, cs.Summary, cs.Messages)
	if sumErr != nil {
		return fmt.Errorf("summarize: %w", sumErr)
	}

	seqNum := nextSessionEventSequence(events)
	if persistErr := persistSummaryCheckpoint(ctx, a.Storage, sessionID, seqNum, seqNum-1, result); persistErr != nil {
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
	ctx = context.WithValue(ctx, toolRoundInputKey{}, userMessage)
	provider := a.modelProvider(ctx)
	if provider == nil {
		return nil, fmt.Errorf("agent %q has no model", a.ID)
	}
	ctx = WithModelProvider(ctx, provider)
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}
	release, err := a.acquireSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer release()

	// Scope per-session harness state (planning tool, VFS) to this session so
	// tools that persist across turns resolve the right session from context.
	ctx = storage.WithSession(ctx, sessionID)
	if journal := agentReplyJournalFromContext(ctx); journal != nil {
		if result := a.Guardrails.CheckInput(ctx, userMessage); result != nil {
			return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
		}
		prior, err := journal.ResumeAgentReply(ctx, a.ID, userMessage, provider.Model())
		if err != nil {
			return nil, fmt.Errorf("resume agent reply: %w", err)
		}
		if prior != nil {
			if prior.Content != "" {
				if result := a.Guardrails.CheckOutput(ctx, prior.Content); result != nil {
					return nil, fmt.Errorf("output guardrail failed: %s", result.Reason)
				}
			}
			if a.OutputSchema != nil && prior.Content != "" {
				if err := validateAgainstSchema(prior.Content, a.OutputSchema); err != nil {
					return nil, fmt.Errorf("output schema validation failed: %w", err)
				}
			}
			return prior, nil
		}
	}

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
	cs := loadChatSession(events)
	if cs.recoveryErr != nil {
		return nil, fmt.Errorf("restore provider continuation: %w", cs.recoveryErr)
	}
	cs.ID = sessionID
	cs.AgentID = a.ID

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// Append user message
	userMsg := model.Message{Role: model.RoleUser, Content: userMessage}
	cs.Messages = append(cs.Messages, userMsg)
	seqNum := nextSessionEventSequence(events)
	if persistErr := persistMessage(ctx, a.Storage, sessionID, seqNum, userMsg); persistErr != nil {
		return nil, fmt.Errorf("persist user message: %w", persistErr)
	}

	// Build the system context (prompt, instructions, memories, knowledge)
	systemMsgs := a.buildSystemContext(ctx, userMessage)

	// Resolve context limit. Use the real BPE tokenizer (WC-A-004 / PLAN.md
	// P1-009) so the compaction trigger and budget reflect actual token counts,
	// not the 4-chars-per-token heuristic.
	counter := model.NewTokenCounter(provider.Model())
	contextLimit := a.resolveContextLimitFor(provider)
	systemTokens := counter.CountTokens(systemMsgs)

	// Check if summarization is needed
	summarizer := model.NewSummarizer(summarizerProvider{agent: a, provider: provider}, counter, model.SummarizationConfig{
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
		if sumPersistErr := persistSummaryCheckpoint(ctx, a.Storage, sessionID, seqNum, seqNum-1, result); sumPersistErr != nil {
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
	if a.ReasoningConfig.Enabled {
		reasoning := a.ReasoningConfig
		req.Reasoning = &reasoning
	}
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

	// Share request-level retries, hooks and tracing with every tool round.
	if journal := toolRoundJournalFromContext(ctx); journal != nil {
		prior, err := journal.ResumeToolRound(ctx, a.ID, userMessage, provider.Model())
		if err != nil {
			return nil, fmt.Errorf("resume tool round: %w", err)
		}
		if len(prior) > 0 {
			req.Messages = prior
		}
	}
	resp, err := a.modelCall(ctx, provider, req, false, func() (*model.ChatResponse, error) {
		return provider.Chat(ctx, req)
	}, nil)
	// Hooks may replace the request slice while trimming context. Carry that
	// working set into tool rounds; the durable session history stays intact.
	messages = req.Messages

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
	recorder := a.newSessionRecorder(sessionID, cs, seqNum, systemMsgs, systemTokens, counter, contextLimit, summarizer)
	loop := a.newToolLoop(ctx, recorder)
	var paused *ToolLoopAction
	for resp.StopReason == model.StopReasonToolCall && len(resp.ToolCalls) > 0 {
		if err := loop.beforeRound(); err != nil {
			return nil, err
		}
		resp, messages, paused, err = a.handleToolCalls(ctx, loop, messages, resp, req)
		if err != nil {
			return nil, err
		}
		if paused != nil {
			resp = pausedResponse(paused)
			break
		}
	}

	// Check output guardrails
	if paused == nil && resp != nil && resp.Content != "" {
		if result := a.Guardrails.CheckOutput(ctx, resp.Content); result != nil {
			return nil, fmt.Errorf("output guardrail failed: %s", result.Reason)
		}
	}

	// Validate response against output schema
	if paused == nil && a.OutputSchema != nil && resp != nil && resp.Content != "" {
		if valErr := validateAgainstSchema(resp.Content, a.OutputSchema); valErr != nil {
			return nil, fmt.Errorf("output schema validation failed: %w", valErr)
		}
	}

	// Persist assistant response
	if resp != nil {
		if pErr := recorder.finish(ctx, model.Message{Role: model.RoleAssistant, Content: resp.Content}); pErr != nil {
			return nil, fmt.Errorf("persist assistant message: %w", pErr)
		}
	}

	// Extract memories (scoped to the agent's tenant)
	if mgr := a.memoryManager(); mgr != nil {
		_ = mgr.ExtractMemories(ctx, cs.Messages)
	}
	if resp != nil && resp.StopReason == model.StopReasonEnd {
		if journal := agentReplyJournalFromContext(ctx); journal != nil {
			if err := journal.CheckpointAgentReply(ctx, a.ID, userMessage, provider.Model(), resp); err != nil {
				return nil, fmt.Errorf("checkpoint agent reply: %w", err)
			}
		}
	}

	return resp, nil
}

// ChatStreamWithSession streams a message within a persistent, multi-turn
// session. It uses the same event-ledger history and context budgeting as
// ChatWithSession, and persists the completed assistant response before the
// stream closes.
func (a *Agent) ChatStreamWithSession(ctx context.Context, sessionID, userMessage string) (<-chan *model.ChatResponse, error) {
	provider := a.modelProvider(ctx)
	if provider == nil {
		return nil, fmt.Errorf("agent %q has no model", a.ID)
	}
	ctx = WithModelProvider(ctx, provider)
	if a.Storage == nil {
		return nil, fmt.Errorf("agent %q has no storage (required for session chat)", a.ID)
	}
	release, err := a.acquireSession(ctx, sessionID)
	if err != nil {
		return nil, err
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
			release()
			return nil, fmt.Errorf("create session: %w", createErr)
		}
	}

	events, err := a.Storage.ListEvents(ctx, sessionID, 0)
	if err != nil {
		release()
		return nil, fmt.Errorf("load session events: %w", err)
	}
	cs := loadChatSession(events)
	if cs.recoveryErr != nil {
		release()
		return nil, fmt.Errorf("restore provider continuation: %w", cs.recoveryErr)
	}
	cs.ID = sessionID
	cs.AgentID = a.ID
	cs.mu.Lock()

	userMsg := model.Message{Role: model.RoleUser, Content: userMessage}
	cs.Messages = append(cs.Messages, userMsg)
	seqNum := nextSessionEventSequence(events)
	if err := persistMessage(ctx, a.Storage, sessionID, seqNum, userMsg); err != nil {
		cs.mu.Unlock()
		release()
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	systemMsgs := a.buildSystemContext(ctx, userMessage)
	counter := model.NewTokenCounter(provider.Model())
	contextLimit := a.resolveContextLimitFor(provider)
	systemTokens := counter.CountTokens(systemMsgs)
	summarizer := model.NewSummarizer(summarizerProvider{agent: a, provider: provider}, counter, model.SummarizationConfig{
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
			release()
			return nil, fmt.Errorf("summarize: %w", sumErr)
		}
		cs.Summary = result.Summary
		cs.Messages = result.PreservedMessages
		seqNum++
		if err := persistSummaryCheckpoint(ctx, a.Storage, sessionID, seqNum, seqNum-1, result); err != nil {
			cs.mu.Unlock()
			release()
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
		release()
		return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
	}

	req := &model.ChatRequest{Messages: messages}
	if a.ReasoningConfig.Enabled {
		reasoning := a.ReasoningConfig
		req.Reasoning = &reasoning
	}
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

	recorder := a.newSessionRecorder(sessionID, cs, seqNum, systemMsgs, systemTokens, counter, contextLimit, summarizer)
	out := make(chan *model.ChatResponse, 64)
	go func() {
		defer close(out)
		defer cs.mu.Unlock()
		defer release()
		resp, _, streamErr := a.streamLoop(ctx, provider, req, messages, out, recorder)
		if streamErr != nil || resp == nil {
			return
		}
		assistantMsg := model.Message{Role: model.RoleAssistant, Content: resp.Content}
		if err := recorder.finish(ctx, assistantMsg); err != nil {
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
	return a.resolveContextLimitFor(a.Model)
}

func (a *Agent) resolveContextLimitFor(provider model.Provider) int {
	if a.ContextCfg.MaxContextTokens > 0 {
		return a.ContextCfg.MaxContextTokens
	}
	return model.ContextLimit(provider.Model(), 0)
}
