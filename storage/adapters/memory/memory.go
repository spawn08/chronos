// Package memory provides an in-memory Storage adapter for Chronos.
// Suitable for testing and development. No external dependencies.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"

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

func (s *Store) CreateSession(_ context.Context, sess *storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sess.ID]; exists {
		return fmt.Errorf("session %q already exists", sess.ID)
	}
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

func (s *Store) GetSession(_ context.Context, id string) (*storage.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	cp := *sess
	return &cp, nil
}

func (s *Store) UpdateSession(_ context.Context, sess *storage.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sess.ID]; !ok {
		return fmt.Errorf("session %q not found", sess.ID)
	}
	cp := *sess
	s.sessions[sess.ID] = &cp
	return nil
}

func (s *Store) ListSessions(_ context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []*storage.Session
	for _, sess := range s.sessions {
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

func (s *Store) PutMemory(_ context.Context, m *storage.MemoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *m
	s.memory[m.ID] = &cp
	return nil
}

func (s *Store) GetMemory(_ context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.memory {
		if m.AgentID == agentID && m.Key == key {
			cp := *m
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("memory key %q not found for agent %q", key, agentID)
}

func (s *Store) ListMemory(_ context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.MemoryRecord
	for _, m := range s.memory {
		if m.AgentID == agentID && m.Kind == kind {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *Store) DeleteMemory(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.memory, id)
	return nil
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(_ context.Context, log *storage.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *log
	s.auditLogs[log.ID] = &cp
	return nil
}

func (s *Store) ListAuditLogs(_ context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var filtered []*storage.AuditLog
	for _, l := range s.auditLogs {
		if l.SessionID == sessionID {
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

func (s *Store) InsertTrace(_ context.Context, t *storage.Trace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	s.traces[t.ID] = &cp
	return nil
}

func (s *Store) GetTrace(_ context.Context, id string) (*storage.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.traces[id]
	if !ok {
		return nil, fmt.Errorf("trace %q not found", id)
	}
	cp := *t
	return &cp, nil
}

func (s *Store) ListTraces(_ context.Context, sessionID string) ([]*storage.Trace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Trace
	for _, t := range s.traces {
		if t.SessionID == sessionID {
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

func (s *Store) AppendEvent(_ context.Context, e *storage.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *e
	s.events[e.ID] = &cp
	return nil
}

func (s *Store) ListEvents(_ context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Event
	for _, e := range s.events {
		if e.SessionID == sessionID && e.SeqNum > afterSeq {
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

func (s *Store) SaveCheckpoint(_ context.Context, cp *storage.Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *cp
	s.checkpoints[cp.ID] = &c
	return nil
}

func (s *Store) GetCheckpoint(_ context.Context, id string) (*storage.Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp, ok := s.checkpoints[id]
	if !ok {
		return nil, fmt.Errorf("checkpoint %q not found", id)
	}
	c := *cp
	return &c, nil
}

func (s *Store) GetLatestCheckpoint(_ context.Context, sessionID string) (*storage.Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *storage.Checkpoint
	for _, cp := range s.checkpoints {
		if cp.SessionID == sessionID {
			if latest == nil || cp.CreatedAt.After(latest.CreatedAt) {
				latest = cp
			}
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("no checkpoint found for session %q", sessionID)
	}
	c := *latest
	return &c, nil
}

func (s *Store) ListCheckpoints(_ context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*storage.Checkpoint
	for _, cp := range s.checkpoints {
		if cp.SessionID == sessionID {
			c := *cp
			out = append(out, &c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SeqNum < out[j].SeqNum
	})
	return out, nil
}
