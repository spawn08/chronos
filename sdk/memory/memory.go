// Package memory provides short-term and long-term memory APIs for agents.
package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spawn08/chronos/storage"
)

// globalUserBucket is the tenant bucket used for long-term memory when no
// userID is set. It keeps the tenantless/global path working while still
// giving every long-term record a stable, scopable namespace.
const globalUserBucket = "_global_"

// ltKeySep separates the tenant bucket from the logical key in a stored
// long-term memory key. Snake_case keys never contain this sequence, so it is
// safe to use as a namespace delimiter.
const ltKeySep = "::"

// Store provides a high-level memory API on top of storage.Storage.
//
// Long-term memory is multi-tenant: it is keyed by (agentID, userID) so that
// different users served by the same agent never observe each other's
// memories. When userID is empty, long-term memory falls back to a documented
// global/tenantless bucket.
type Store struct {
	agentID string
	userID  string
	backend storage.Storage
}

// NewStore creates a memory store for the given agent. The store is scoped to
// the global/tenantless long-term bucket; use NewStoreForUser or ForUser to
// scope it to a specific tenant.
func NewStore(agentID string, backend storage.Storage) *Store {
	return &Store{agentID: agentID, backend: backend}
}

// NewStoreForUser creates a memory store scoped to a specific user (tenant).
// An empty userID selects the global/tenantless long-term bucket.
func NewStoreForUser(agentID, userID string, backend storage.Storage) *Store {
	return &Store{agentID: agentID, userID: userID, backend: backend}
}

// ForUser returns a shallow copy of the store scoped to the given user. The
// original store is left unchanged, so a shared agent-level store can be safely
// re-scoped per request. An empty userID selects the global/tenantless bucket.
func (s *Store) ForUser(userID string) *Store {
	if s == nil {
		return nil
	}
	cp := *s
	cp.userID = userID
	return &cp
}

// UserID reports the tenant this store is scoped to ("" means global).
func (s *Store) UserID() string { return s.userID }

// userBucket returns the tenant bucket used for namespacing long-term memory.
func (s *Store) userBucket() string {
	if s.userID == "" {
		return globalUserBucket
	}
	return s.userID
}

// longTermKey namespaces a logical key with the current tenant bucket so that
// same-named keys from different tenants do not collide in storage.
func (s *Store) longTermKey(key string) string {
	return s.userBucket() + ltKeySep + key
}

// longTermID builds the storage ID for a long-term record, scoped by
// (agentID, userBucket, key).
func (s *Store) longTermID(key string) string {
	return fmt.Sprintf("mem_%s_%s_lt_%s", s.agentID, s.userBucket(), key)
}

// SetShortTerm stores a value in session-scoped working memory.
func (s *Store) SetShortTerm(ctx context.Context, sessionID, key string, value any) error {
	return s.backend.PutMemory(ctx, &storage.MemoryRecord{
		ID:        fmt.Sprintf("mem_%s_%s_%s", s.agentID, sessionID, key),
		SessionID: sessionID,
		AgentID:   s.agentID,
		UserID:    s.userID,
		Kind:      "short_term",
		Key:       key,
		Value:     value,
		CreatedAt: time.Now(),
	})
}

// SetLongTerm stores a value in cross-session persistent memory, scoped to the
// store's tenant (agentID, userID).
func (s *Store) SetLongTerm(ctx context.Context, key string, value any) error {
	return s.backend.PutMemory(ctx, &storage.MemoryRecord{
		ID:        s.longTermID(key),
		AgentID:   s.agentID,
		UserID:    s.userID,
		Kind:      "long_term",
		Key:       s.longTermKey(key),
		Value:     value,
		CreatedAt: time.Now(),
	})
}

// DeleteLongTerm removes a long-term memory for the store's tenant by key.
func (s *Store) DeleteLongTerm(ctx context.Context, key string) error {
	return s.backend.DeleteMemory(ctx, s.longTermID(key))
}

// Get retrieves a memory value by logical key. It resolves the tenant-scoped
// long-term key first and falls back to the raw key for short-term or legacy
// records.
func (s *Store) Get(ctx context.Context, key string) (any, error) {
	rec, err := s.backend.GetMemory(ctx, s.agentID, s.longTermKey(key))
	if err == nil {
		return rec.Value, nil
	}
	// Fall back to the raw key (short-term or pre-namespacing records).
	if rec, rawErr := s.backend.GetMemory(ctx, s.agentID, key); rawErr == nil {
		return rec.Value, nil
	}
	return nil, err
}

// ListShortTerm returns all short-term memories for this agent.
func (s *Store) ListShortTerm(ctx context.Context) ([]*storage.MemoryRecord, error) {
	return s.backend.ListMemory(ctx, s.agentID, "short_term")
}

// ListLongTerm returns the long-term memories that belong to the store's
// tenant. Records are returned with their logical (un-namespaced) key restored.
func (s *Store) ListLongTerm(ctx context.Context) ([]*storage.MemoryRecord, error) {
	all, err := s.backend.ListMemory(ctx, s.agentID, "long_term")
	if err != nil {
		return nil, err
	}
	prefix := s.userBucket() + ltKeySep
	var out []*storage.MemoryRecord
	for _, m := range all {
		if !strings.HasPrefix(m.Key, prefix) {
			continue
		}
		cp := *m
		cp.Key = strings.TrimPrefix(m.Key, prefix)
		cp.UserID = s.userID
		out = append(out, &cp)
	}
	return out, nil
}
