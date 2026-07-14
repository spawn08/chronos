// Package memory provides short-term and long-term memory APIs for agents.
package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/spawn08/chronos/storage"
)

// ltKeySep separates the encoded tenant token from the logical key in a stored
// long-term memory key. The token (see bucketToken) is base64url, which never
// contains ':' — so this delimiter is unambiguous even when the logical key or
// the userID itself contains "::". This closes the cross-tenant read that a raw
// "userBucket::key" scheme allowed when userID/key were attacker-influenced.
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

// bucketToken returns a collision-free token identifying the tenant this store
// is scoped to. The pre-encoding source is tagged ("g" for the global/tenantless
// bucket, "u:"+userID for a named tenant) and then base64url-encoded. Tagging
// keeps the empty-userID bucket distinct from a tenant literally named
// "_global_", and base64 encoding both makes the token free of the ltKeySep
// delimiter and injective in userID — so no two distinct tenants (and no tenant
// vs. global) can ever share a namespace.
func (s *Store) bucketToken() string {
	src := "g"
	if s.userID != "" {
		src = "u:" + s.userID
	}
	return base64.RawURLEncoding.EncodeToString([]byte(src))
}

// longTermKey namespaces a logical key with the current tenant token so that
// same-named keys from different tenants do not collide in storage. Because the
// token contains no ':' and the logical key is appended verbatim after the
// delimiter, the boundary is unambiguous regardless of the key's contents.
func (s *Store) longTermKey(key string) string {
	return s.bucketToken() + ltKeySep + key
}

// longTermID builds the storage ID for a long-term record, scoped by
// (agentID, tenant token, key).
func (s *Store) longTermID(key string) string {
	return fmt.Sprintf("mem_%s_%s_lt_%s", s.agentID, s.bucketToken(), key)
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
	// Fall back to the raw key for short-term (session) records, which are
	// stored un-namespaced. Long-term records resolve only through the
	// tenant-scoped key above, so the fallback must never return a long_term
	// record — doing so would reopen a cross-tenant/legacy read path (P0-008).
	if rec, rawErr := s.backend.GetMemory(ctx, s.agentID, key); rawErr == nil && rec.Kind != "long_term" {
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
	prefix := s.bucketToken() + ltKeySep
	var out []*storage.MemoryRecord
	for _, m := range all {
		// HasPrefix is boundary-safe here: the token contains no ':' so no other
		// tenant's namespaced key can begin with this exact "token::" prefix.
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
