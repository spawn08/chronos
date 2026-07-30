// Package memory — manager.go provides an LLM-powered memory manager inspired by Agno's MemoryManager.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// Recall tuning. Because storage.VectorStore.Search cannot filter by tenant
// scope, Recall over-fetches and then post-filters to the caller's bucket, so
// the requested top-k survives even when a collection mixes tenants.
const (
	defaultRecallTopK = 5
	recallOverFetch   = 5
	recallMinFetch    = 20
)

// Manager uses an LLM to autonomously decide what to remember from conversations.
//
// When a vector index is attached via WithVectorIndex, memories are additionally
// embedded on write and can be retrieved by semantic relevance via Recall. The
// vector index is optional: without it the Manager behaves exactly as before
// (write-through to the relational store, full-dump recall via GetUserMemories).
type Manager struct {
	store   *Store
	model   model.Provider
	userID  string
	agentID string

	// Optional semantic index (all nil unless WithVectorIndex is used).
	embedder   model.EmbeddingsProvider
	vstore     storage.VectorStore
	embedModel string
	collection string
	dimension  int
}

// RecalledMemory is one semantically-retrieved long-term memory, ranked by Score
// (higher is more relevant). It is a structured candidate — never a pre-formatted
// string — so a caller (e.g. the agent's context budget) can rank, trim, or
// format it as needed.
type RecalledMemory struct {
	Key     string  `json:"key"`
	Value   any     `json:"value"`
	Content string  `json:"content"`
	Score   float32 `json:"score"`
}

// NewManager creates an LLM-powered memory manager. The underlying store is
// scoped to userID so that all reads and writes are isolated per tenant. An
// empty userID uses the global/tenantless bucket.
func NewManager(agentID, userID string, store *Store, provider model.Provider) *Manager {
	if store != nil {
		store = store.ForUser(userID)
	}
	return &Manager{
		store:   store,
		model:   provider,
		userID:  userID,
		agentID: agentID,
	}
}

// UserID reports the tenant this manager is scoped to ("" means global).
func (m *Manager) UserID() string { return m.userID }

// WithUserID returns a copy of the manager scoped to a different tenant,
// re-scoping the underlying store as well. The original manager is unchanged,
// so a shared agent-level manager can be safely re-scoped per request.
func (m *Manager) WithUserID(userID string) *Manager {
	cp := *m
	cp.userID = userID
	cp.store = m.store.ForUser(userID)
	return &cp
}

// WithVectorIndex returns a copy of the manager with a semantic index attached,
// enabling embed-on-write and Recall. The collection is per-agent; tenant
// isolation within it is enforced by Recall's scope filter, not by the
// collection name. The original manager is unchanged, and the returned copy is
// safe to re-scope with WithUserID (the index fields survive the shallow copy),
// so a shared agent-level manager can be indexed once and re-scoped per request.
func (m *Manager) WithVectorIndex(embedder model.EmbeddingsProvider, vstore storage.VectorStore, embedModel string, dimension int) *Manager {
	cp := *m
	cp.embedder = embedder
	cp.vstore = vstore
	cp.embedModel = embedModel
	cp.dimension = dimension
	cp.collection = "mem_" + m.agentID
	return &cp
}

// CanRecall reports whether a semantic index is attached (WithVectorIndex).
func (m *Manager) CanRecall() bool {
	return m.embedder != nil && m.vstore != nil
}

// Recall semantically retrieves the top-k long-term memories most relevant to
// query, scoped to this manager's tenant. It returns candidates ranked by
// descending score. When no index is attached it returns (nil, nil); a missing
// collection or a search error also degrades gracefully to no memories, matching
// the best-effort injection at the agent's call sites.
func (m *Manager) Recall(ctx context.Context, query string, topK int) ([]RecalledMemory, error) {
	if !m.CanRecall() || query == "" {
		return nil, nil
	}
	if topK <= 0 {
		topK = defaultRecallTopK
	}

	vec, err := m.embedQuery(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("memory manager: recall embed: %w", err)
	}

	// Over-fetch: Search has no tenant filter, so ask for more than topK and drop
	// out-of-scope hits below, guaranteeing no cross-tenant recall.
	fetch := topK * recallOverFetch
	if fetch < recallMinFetch {
		fetch = recallMinFetch
	}
	results, err := m.vstore.Search(ctx, m.collection, vec, fetch)
	if err != nil {
		// The collection may not exist until the first memory is indexed.
		return nil, nil
	}

	scope := m.store.bucketToken()
	out := make([]RecalledMemory, 0, topK)
	for i := range results {
		md := results[i].Metadata
		if md == nil || md["scope"] != scope {
			continue
		}
		key, _ := md["key"].(string)
		out = append(out, RecalledMemory{
			Key:     key,
			Value:   md["value"],
			Content: results[i].Content,
			Score:   results[i].Score,
		})
		if len(out) >= topK {
			break
		}
	}
	return out, nil
}

// embedQuery embeds a single text with the configured embedding model.
func (m *Manager) embedQuery(ctx context.Context, text string) ([]float32, error) {
	resp, err := m.embedder.Embed(ctx, &model.EmbeddingRequest{Model: m.embedModel, Input: []string{text}})
	if err != nil {
		return nil, err
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return resp.Embeddings[0], nil
}

// indexMemories embeds the given (key, value) memories and upserts them into the
// vector index, scoped to this manager's tenant. It is a no-op without an
// attached index or when there is nothing to index. Vector IDs equal the
// relational long-term ID, so re-indexing an existing key overwrites its vector.
func (m *Manager) indexMemories(ctx context.Context, keys []string, values []any) error {
	if !m.CanRecall() || len(keys) == 0 {
		return nil
	}
	if err := m.vstore.CreateCollection(ctx, m.collection, m.dimension); err != nil {
		return fmt.Errorf("memory manager: index create collection: %w", err)
	}

	texts := make([]string, len(keys))
	for i := range keys {
		texts[i] = fmt.Sprintf("%s: %v", keys[i], values[i])
	}
	resp, err := m.embedder.Embed(ctx, &model.EmbeddingRequest{Model: m.embedModel, Input: texts})
	if err != nil {
		return fmt.Errorf("memory manager: index embed: %w", err)
	}
	if len(resp.Embeddings) != len(keys) {
		return fmt.Errorf("memory manager: index embed: got %d embeddings for %d memories", len(resp.Embeddings), len(keys))
	}

	scope := m.store.bucketToken()
	embs := make([]storage.Embedding, len(keys))
	for i := range keys {
		embs[i] = storage.Embedding{
			ID:      m.store.longTermID(keys[i]),
			Vector:  resp.Embeddings[i],
			Content: texts[i],
			Metadata: map[string]any{
				"scope":    scope,
				"key":      keys[i],
				"value":    values[i],
				"agent_id": m.agentID,
			},
		}
	}
	if err := m.vstore.Upsert(ctx, m.collection, embs); err != nil {
		return fmt.Errorf("memory manager: index upsert: %w", err)
	}
	return nil
}

// unindexMemories removes vectors for the given long-term IDs. It is a no-op
// without an attached index or when there is nothing to remove.
func (m *Manager) unindexMemories(ctx context.Context, ids []string) error {
	if !m.CanRecall() || len(ids) == 0 {
		return nil
	}
	if err := m.vstore.Delete(ctx, m.collection, ids); err != nil {
		return fmt.Errorf("memory manager: unindex: %w", err)
	}
	return nil
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
	for i := range messages {
		convo += fmt.Sprintf("%s: %s\n", messages[i].Role, messages[i].Content)
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

	keys := make([]string, 0, len(memories))
	values := make([]any, 0, len(memories))
	for _, mem := range memories {
		if err := m.store.SetLongTerm(ctx, mem.Key, mem.Value); err != nil {
			return err
		}
		keys = append(keys, mem.Key)
		values = append(values, mem.Value)
	}
	// Mirror the newly-stored facts into the semantic index (no-op without one).
	return m.indexMemories(ctx, keys, values)
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

	// Clear and re-store optimized memories, keeping the semantic index in sync.
	// The vector ID equals the relational record ID (both are longTermID(key)),
	// so old vectors are removed by the same IDs that delete the records.
	oldIDs := make([]string, 0, len(existing))
	for _, old := range existing {
		_ = m.store.backend.DeleteMemory(ctx, old.ID)
		oldIDs = append(oldIDs, old.ID)
	}
	_ = m.unindexMemories(ctx, oldIDs)

	keys := make([]string, 0, len(optimized))
	values := make([]any, 0, len(optimized))
	for _, mem := range optimized {
		_ = m.store.SetLongTerm(ctx, mem.Key, mem.Value)
		keys = append(keys, mem.Key)
		values = append(values, mem.Value)
	}
	_ = m.indexMemories(ctx, keys, values)
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
				if err := m.store.SetLongTerm(ctx, key, value); err != nil {
					return nil, err
				}
				return nil, m.indexMemories(ctx, []string{key}, []any{value})
			},
		},
		{
			Name:        "forget",
			Description: "Remove a stored memory by key",
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				key, _ := args["key"].(string)
				if err := m.store.DeleteLongTerm(ctx, key); err != nil {
					return nil, err
				}
				return nil, m.unindexMemories(ctx, []string{m.store.longTermID(key)})
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
