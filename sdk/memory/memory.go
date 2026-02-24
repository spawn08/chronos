// Package memory provides short-term and long-term memory APIs for agents.
package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Store provides a high-level memory API on top of storage.Storage.
type Store struct {
	agentID string
	backend storage.Storage
}

// NewStore creates a memory store for the given agent.
func NewStore(agentID string, backend storage.Storage) *Store {
	return &Store{agentID: agentID, backend: backend}
}

// SetShortTerm stores a value in session-scoped working memory.
func (s *Store) SetShortTerm(ctx context.Context, sessionID, key string, value any) error {
	return s.backend.PutMemory(ctx, &storage.MemoryRecord{
		ID:        fmt.Sprintf("mem_%s_%s_%s", s.agentID, sessionID, key),
		SessionID: sessionID,
		AgentID:   s.agentID,
		Kind:      "short_term",
		Key:       key,
		Value:     value,
		CreatedAt: time.Now(),
	})
}

// SetLongTerm stores a value in cross-session persistent memory.
func (s *Store) SetLongTerm(ctx context.Context, key string, value any) error {
	return s.backend.PutMemory(ctx, &storage.MemoryRecord{
		ID:        fmt.Sprintf("mem_%s_lt_%s", s.agentID, key),
		AgentID:   s.agentID,
		Kind:      "long_term",
		Key:       key,
		Value:     value,
		CreatedAt: time.Now(),
	})
}

// Get retrieves a memory value by key.
func (s *Store) Get(ctx context.Context, key string) (any, error) {
	rec, err := s.backend.GetMemory(ctx, s.agentID, key)
	if err != nil {
		return nil, err
	}
	return rec.Value, nil
}

// ListShortTerm returns all short-term memories for this agent.
func (s *Store) ListShortTerm(ctx context.Context) ([]*storage.MemoryRecord, error) {
	return s.backend.ListMemory(ctx, s.agentID, "short_term")
}

// ListLongTerm returns all long-term memories for this agent.
func (s *Store) ListLongTerm(ctx context.Context) ([]*storage.MemoryRecord, error) {
	return s.backend.ListMemory(ctx, s.agentID, "long_term")
}
