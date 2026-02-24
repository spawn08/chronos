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
	if err := persistMessage(ctx, a.Storage, sessionID, seqNum, userMsg); err != nil {
		return nil, fmt.Errorf("persist user message: %w", err)
	}

	// Build the system context (prompt, instructions, memories, knowledge)
	systemMsgs := a.buildSystemContext(ctx, userMessage)

	// Resolve context limit
	counter := model.NewEstimatingCounter()
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
		if err := persistSummary(ctx, a.Storage, sessionID, seqNum, cs.Summary); err != nil {
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

	// Build final message array
	messages := make([]model.Message, 0, len(systemMsgs)+len(cs.Messages)+1)
	messages = append(messages, systemMsgs...)
	if cs.Summary != "" {
		messages = append(messages, model.Message{
			Role:    model.RoleSystem,
			Content: "Previous conversation summary:\n" + cs.Summary,
		})
	}
	messages = append(messages, cs.Messages...)

	// Check input guardrails
	if result := a.Guardrails.CheckInput(ctx, userMessage); result != nil {
		return nil, fmt.Errorf("input guardrail failed: %s", result.Reason)
	}

	req := &model.ChatRequest{Messages: messages}
	if a.OutputSchema != nil {
		req.ResponseFormat = "json_object"
	}

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

	// Fire model call hooks
	modelEvt := &hooks.Event{Type: hooks.EventModelCallBefore, Name: a.Model.Name(), Input: req}
	if err := a.Hooks.Before(ctx, modelEvt); err != nil {
		return nil, fmt.Errorf("hook before model call: %w", err)
	}

	resp, err := a.Model.Chat(ctx, req)

	modelEvt.Type = hooks.EventModelCallAfter
	modelEvt.Output = resp
	modelEvt.Error = err
	_ = a.Hooks.After(ctx, modelEvt)

	if err != nil {
		return nil, fmt.Errorf("agent %q session chat: %w", a.ID, err)
	}

	// Handle tool calls
	if resp.StopReason == model.StopReasonToolCall && len(resp.ToolCalls) > 0 {
		resp, err = a.handleToolCalls(ctx, messages, resp)
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

	// Persist assistant response
	if resp != nil {
		assistantMsg := model.Message{Role: model.RoleAssistant, Content: resp.Content}
		cs.Messages = append(cs.Messages, assistantMsg)
		seqNum++
		if pErr := persistMessage(ctx, a.Storage, sessionID, seqNum, assistantMsg); pErr != nil {
			return nil, fmt.Errorf("persist assistant message: %w", pErr)
		}
	}

	// Extract memories
	if a.MemoryManager != nil {
		_ = a.MemoryManager.ExtractMemories(ctx, cs.Messages)
	}

	return resp, nil
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
	if a.MemoryManager != nil {
		if memCtx, err := a.MemoryManager.GetUserMemories(ctx); err == nil && memCtx != "" {
			messages = append(messages, model.Message{Role: model.RoleSystem, Content: memCtx})
		}
	}
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

// chatMessagePayload is used for JSON marshalling of chat message events.
type chatMessagePayload struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCallJSON `json:"tool_calls,omitempty"`
}

type toolCallJSON struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// marshalChatPayload produces a JSON-safe payload from a message.
func marshalChatPayload(msg model.Message) map[string]any {
	p := map[string]any{"role": msg.Role, "content": msg.Content}
	if msg.Name != "" {
		p["name"] = msg.Name
	}
	if msg.ToolCallID != "" {
		p["tool_call_id"] = msg.ToolCallID
	}
	if len(msg.ToolCalls) > 0 {
		tcs := make([]map[string]any, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			tcs[i] = map[string]any{"id": tc.ID, "name": tc.Name, "arguments": tc.Arguments}
		}
		p["tool_calls"] = tcs
	}
	return p
}

// unmarshalChatPayload reconstructs a Message from a JSON payload map.
func unmarshalChatPayload(data []byte) (model.Message, error) {
	var p chatMessagePayload
	if err := json.Unmarshal(data, &p); err != nil {
		return model.Message{}, err
	}
	msg := model.Message{
		Role:       p.Role,
		Content:    p.Content,
		Name:       p.Name,
		ToolCallID: p.ToolCallID,
	}
	for _, tc := range p.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}
	return msg, nil
}
