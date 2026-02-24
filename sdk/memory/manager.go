// Package memory — manager.go provides an LLM-powered memory manager inspired by Agno's MemoryManager.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/model"
)

// Ensure storage is used via m.store.backend (which is storage.Storage)

// Manager uses an LLM to autonomously decide what to remember from conversations.
type Manager struct {
	store   *Store
	model   model.Provider
	userID  string
	agentID string
}

// NewManager creates an LLM-powered memory manager.
func NewManager(agentID, userID string, store *Store, provider model.Provider) *Manager {
	return &Manager{
		store:   store,
		model:   provider,
		userID:  userID,
		agentID: agentID,
	}
}

const memorySystemPrompt = `You are a memory manager. Given a conversation, decide what facts are worth remembering about the user for future conversations.

Respond with a JSON array of memory objects to store. Each object should have:
- "key": a short snake_case identifier
- "value": the fact to remember

If nothing is worth remembering, respond with an empty array [].
Only extract clear, factual information — not opinions or speculation.`

// ExtractMemories uses the LLM to identify memorable facts from messages and stores them.
func (m *Manager) ExtractMemories(ctx context.Context, messages []model.Message) error {
	// Build the conversation text for the model
	convo := ""
	for _, msg := range messages {
		convo += fmt.Sprintf("%s: %s\n", msg.Role, msg.Content)
	}

	resp, err := m.model.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{
			{Role: "system", Content: memorySystemPrompt},
			{Role: "user", Content: convo},
		},
		Temperature: 0.0,
	})
	if err != nil {
		return fmt.Errorf("memory manager: extract: %w", err)
	}

	var memories []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &memories); err != nil {
		// Model may not have returned valid JSON — skip gracefully
		return nil
	}

	for _, mem := range memories {
		if err := m.store.SetLongTerm(ctx, mem.Key, mem.Value); err != nil {
			return err
		}
	}
	return nil
}

// OptimizeMemories asks the LLM to compress/deduplicate existing long-term memories.
func (m *Manager) OptimizeMemories(ctx context.Context) error {
	existing, err := m.store.ListLongTerm(ctx)
	if err != nil {
		return err
	}
	if len(existing) < 5 {
		return nil // not enough to optimize
	}

	memJSON, _ := json.Marshal(existing)
	resp, err := m.model.Chat(ctx, &model.ChatRequest{
		Messages: []model.Message{
			{Role: "system", Content: "You are a memory optimizer. Given a list of memories, merge duplicates and remove outdated entries. Return a JSON array of the optimized memories with 'key' and 'value' fields."},
			{Role: "user", Content: string(memJSON)},
		},
		Temperature: 0.0,
	})
	if err != nil {
		return fmt.Errorf("memory manager: optimize: %w", err)
	}

	var optimized []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &optimized); err != nil {
		return nil
	}

	// Clear and re-store optimized memories
	for _, old := range existing {
		_ = m.store.backend.DeleteMemory(ctx, old.ID)
	}
	for _, mem := range optimized {
		_ = m.store.SetLongTerm(ctx, mem.Key, mem.Value)
	}
	return nil
}

// GetUserMemories returns all long-term memories, formatted for context injection.
func (m *Manager) GetUserMemories(ctx context.Context) (string, error) {
	memories, err := m.store.ListLongTerm(ctx)
	if err != nil {
		return "", err
	}
	if len(memories) == 0 {
		return "", nil
	}

	result := "User memories:\n"
	for _, mem := range memories {
		result += fmt.Sprintf("- %s: %v\n", mem.Key, mem.Value)
	}
	return result, nil
}

// MemoryTools returns tool definitions that let the model manage memory directly (agentic memory).
func (m *Manager) MemoryTools() []MemoryTool {
	return []MemoryTool{
		{
			Name:        "remember",
			Description: "Store a fact about the user for future conversations",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				key, _ := args["key"].(string)
				value := args["value"]
				if key == "" {
					return nil, fmt.Errorf("key is required")
				}
				return nil, m.store.SetLongTerm(ctx, key, value)
			},
		},
		{
			Name:        "forget",
			Description: "Remove a stored memory by key",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				key, _ := args["key"].(string)
				id := fmt.Sprintf("mem_%s_lt_%s", m.agentID, key)
				return nil, m.store.backend.DeleteMemory(ctx, id)
			},
		},
		{
			Name:        "recall",
			Description: "List all stored memories about the user",
			Handler: func(ctx context.Context, _ map[string]any) (any, error) {
				mems, err := m.store.ListLongTerm(ctx)
				if err != nil {
					return nil, err
				}
				result := make([]map[string]any, len(mems))
				for i, mem := range mems {
					result[i] = map[string]any{"key": mem.Key, "value": mem.Value, "created_at": mem.CreatedAt.Format(time.RFC3339)}
				}
				return result, nil
			},
		},
	}
}

// MemoryTool is a tool definition for agentic memory management.
type MemoryTool struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args map[string]any) (any, error)
}
