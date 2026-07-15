package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// KeyRecord is the persisted metadata for an API key. The raw key is never
// stored; only its hash (see HashKey) is retained.
type KeyRecord struct {
	// ID is a stable, non-secret identifier/label for the key.
	ID string `json:"id"`
	// Hash is the hex-encoded SHA-256 of the raw key.
	Hash string `json:"hash"`
	// Scope is the RBAC scope/role granted to callers using this key.
	Scope string `json:"scope"`
	// UserID identifies the principal the key acts as.
	UserID string `json:"user_id"`
	// TenantID identifies the tenant for per-tenant quota accounting.
	TenantID string `json:"tenant_id,omitempty"`
	// Quota, when non-zero, is enforced per key on each request.
	Quota Quota `json:"quota,omitempty"`
	// Disabled revokes the key without deleting its record.
	Disabled bool `json:"disabled,omitempty"`
	// CreatedAt records when the key was added.
	CreatedAt time.Time `json:"created_at"`
}

// KeyStore is a persisted, hashed API-key store. Implementations must never
// retain the raw key material and must compare using constant-time operations.
// The control plane can wire a database-backed implementation; MemoryKeyStore
// is the default.
type KeyStore interface {
	// Lookup resolves the raw key to its record using constant-time comparison.
	// It returns (record, true, nil) on match, (nil, false, nil) when no active
	// key matches, and a non-nil error only on backend failure.
	Lookup(ctx context.Context, rawKey string) (*KeyRecord, bool, error)
	// Add hashes rawKey and stores the record; the returned record has Hash and
	// CreatedAt populated. The raw key is not retained.
	Add(ctx context.Context, rawKey string, meta KeyRecord) (KeyRecord, error)
	// Remove deletes the record with the given ID.
	Remove(ctx context.Context, id string) error
	// List returns all stored records (without raw keys).
	List(ctx context.Context) ([]KeyRecord, error)
}

// HashKey returns the hex-encoded SHA-256 digest of a raw API key. SHA-256 is
// appropriate here because API keys are high-entropy secrets (unlike
// low-entropy passwords, which would warrant bcrypt/argon2).
func HashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// MemoryKeyStore is an in-memory KeyStore keyed by record ID. It is safe for
// concurrent use.
type MemoryKeyStore struct {
	mu      sync.RWMutex
	records map[string]KeyRecord
	now     func() time.Time
}

// NewMemoryKeyStore returns an empty in-memory key store.
func NewMemoryKeyStore() *MemoryKeyStore {
	return &MemoryKeyStore{
		records: make(map[string]KeyRecord),
		now:     time.Now,
	}
}

// Add hashes and stores the key. If meta.ID is empty, the key hash is used as
// the ID.
func (s *MemoryKeyStore) Add(_ context.Context, rawKey string, meta KeyRecord) (KeyRecord, error) {
	if rawKey == "" {
		return KeyRecord{}, fmt.Errorf("raw key must not be empty")
	}
	rec := meta
	rec.Hash = HashKey(rawKey)
	if rec.ID == "" {
		rec.ID = rec.Hash
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = s.now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ID] = rec
	return rec, nil
}

// Lookup matches the raw key against all active records using constant-time
// comparison, without early exit, so match timing does not depend on which
// record (if any) matched.
func (s *MemoryKeyStore) Lookup(_ context.Context, rawKey string) (*KeyRecord, bool, error) {
	candidate := HashKey(rawKey)
	candidateBytes := []byte(candidate)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matched *KeyRecord
	for id := range s.records {
		rec := s.records[id]
		if rec.Disabled {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(rec.Hash), candidateBytes) == 1 {
			r := rec
			matched = &r
			// Do not break: keep the loop's work independent of the match.
		}
	}
	if matched == nil {
		return nil, false, nil
	}
	return matched, true, nil
}

// Remove deletes a record by ID.
func (s *MemoryKeyStore) Remove(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

// List returns a snapshot of all records.
func (s *MemoryKeyStore) List(_ context.Context) ([]KeyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]KeyRecord, 0, len(s.records))
	for id := range s.records {
		out = append(out, s.records[id])
	}
	return out, nil
}
