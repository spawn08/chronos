// Package memory provides an in-memory Storage adapter for Chronos.
// Suitable for testing and development. No external dependencies.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.Storage using Go maps with sync.RWMutex.
type Store struct {
	mu          sync.RWMutex
	sessions    map[string]*storage.Session
	memory      map[string]*storage.MemoryRecord
	auditLogs   map[string]*storage.AuditLog
	traces      map[string]*storage.Trace
	events      map[string]*storage.Event
	checkpoints map[string]*storage.Checkpoint
}

// New creates a new in-memory storage.
func New() *Store {
	return &Store{
		sessions:    make(map[string]*storage.Session),
		memory:      make(map[string]*storage.MemoryRecord),
		auditLogs:   make(map[string]*storage.AuditLog),
		traces:      make(map[string]*storage.Trace),
		events:      make(map[string]*storage.Event),
		checkpoints: make(map[string]*storage.Checkpoint),
	}
}

func (s *Store) Migrate(_ context.Context) error { return nil }
func (s *Store) Close() error                    { return nil }

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sess.ID]; exists {
		return fmt.Errorf("session %q already exists", sess.ID)
	}
	cp := *sess
	cp.TenantID = tenant
	s.sessions[sess.ID] = &cp
	return nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok || sess.TenantID != tenant {
		return nil, fmt.Errorf("session %q not found", id)
	}
	cp := *sess
	return &cp, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.sessions[sess.ID]
	if !ok || existing.TenantID != tenant {
		return fmt.Errorf("session %q not found", sess.ID)
	}
	cp := *sess
	cp.TenantID = tenant
	s.sessions[sess.ID] = &cp
	return nil
}

// UpdateSessionStatus implements storage.SessionStatusUpdater: a narrow write
// that only touches Status/UpdatedAt, so it can never race with (and clobber)
// a concurrent Metadata writer the way a GetSession->mutate->UpdateSession
// read-modify-write could.
func (s *Store) UpdateSessionStatus(ctx context.Context, sessionID, status string, updatedAt time.Time) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok || sess.TenantID != tenant {
		return fmt.Errorf("session %q not found", sessionID)
	}
	sess.Status = status
	sess.UpdatedAt = updatedAt
	return nil
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []*storage.Session
	for _, sess := range s.sessions {
		if sess.TenantID != tenant {
			continue
		}
		if agentID == "" || sess.AgentID == agentID {
			cp := *sess
			filtered = append(filtered, &cp)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if offset >= len(filtered) {
		return nil, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	cp.TenantID = tenant
	s.memory[m.ID] = &cp
	return nil
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.memory {
		if m.TenantID == tenant && m.AgentID == agentID && m.Key == key {
			cp := *m
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("memory key %q not found for agent %q", key, agentID)
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.MemoryRecord
	for _, m := range s.memory {
		if m.TenantID == tenant && m.AgentID == agentID && m.Kind == kind {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.memory[id]; ok && m.TenantID == tenant {
		delete(s.memory, id)
	}
	return nil
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *log
	cp.TenantID = tenant
	s.auditLogs[log.ID] = &cp
	return nil
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []*storage.AuditLog
	for _, l := range s.auditLogs {
		if l.TenantID == tenant && l.SessionID == sessionID {
			cp := *l
			filtered = append(filtered, &cp)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if offset >= len(filtered) {
		return nil, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	cp.TenantID = tenant
	s.traces[t.ID] = &cp
	return nil
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.traces[id]
	if !ok || t.TenantID != tenant {
		return nil, fmt.Errorf("trace %q not found", id)
	}
	cp := *t
	return &cp, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Trace
	for _, t := range s.traces {
		if t.TenantID == tenant && t.SessionID == sessionID {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	cp.TenantID = tenant
	s.events[e.ID] = &cp
	return nil
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Event
	for _, e := range s.events {
		if e.TenantID == tenant && e.SessionID == sessionID && e.SeqNum > afterSeq {
			cp := *e
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SeqNum < out[j].SeqNum
	})
	return out, nil
}

// --- Checkpoints ---

// cloneCheckpointState returns an independent copy of a checkpoint's State
// map. The graph runner (engine/graph) mutates its State map in place and
// reuses the same map object across every checkpoint of a run, so a bare
// struct copy (`c := *cp`) would leave every stored checkpoint's State
// aliasing that one mutable map — later mutations would then leak backward
// into "past" checkpoints, silently breaking time-travel. SQL-backed adapters
// don't have this problem because SaveCheckpoint serializes State to JSON
// bytes at call time, an implicit deep copy.
func cloneCheckpointState(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	tenant := storage.TenantFromContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *cp
	c.TenantID = tenant
	c.State = cloneCheckpointState(cp.State)
	s.checkpoints[cp.ID] = &c
	return nil
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.checkpoints[id]
	if !ok || cp.TenantID != tenant {
		return nil, fmt.Errorf("checkpoint %q not found", id)
	}
	c := *cp
	c.State = cloneCheckpointState(cp.State)
	return &c, nil
}

// GetLatestCheckpoint returns the checkpoint with the highest seq_num for the
// session, not the one with the latest wall-clock CreatedAt: same-tick
// checkpoints (or a clock step) would otherwise make the "latest" checkpoint
// non-deterministic, the bug PLAN.md P0-003 fixed for the SQL adapters — this
// applies the same fix here.
func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *storage.Checkpoint
	for _, cp := range s.checkpoints {
		if cp.TenantID == tenant && cp.SessionID == sessionID {
			if latest == nil || cp.SeqNum > latest.SeqNum {
				latest = cp
			}
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no checkpoint found for session %q", sessionID)
	}
	c := *latest
	c.State = cloneCheckpointState(latest.State)
	return &c, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	tenant := storage.TenantFromContext(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Checkpoint
	for _, cp := range s.checkpoints {
		if cp.TenantID == tenant && cp.SessionID == sessionID {
			c := *cp
			c.State = cloneCheckpointState(cp.State)
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SeqNum < out[j].SeqNum
	})
	return out, nil
}
