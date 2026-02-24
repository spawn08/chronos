// Package redis provides a Redis-backed Storage adapter for Chronos.
// Uses the Redis RESP protocol via a minimal TCP client.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.Storage using Redis.
type Store struct {
	addr     string
	password string
	db       int
	mu       sync.Mutex
	conn     net.Conn
}

// New creates a Redis storage adapter.
func New(addr, password string, db int) (*Store, error) {
	s := &Store{addr: addr, password: password, db: db}
	if err := s.connect(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) connect() error {
	conn, err := net.DialTimeout("tcp", s.addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("redis connect: %w", err)
	}
	s.conn = conn
	if s.password != "" {
		_ = s.rawCmd("AUTH", s.password)
	}
	if s.db != 0 {
		_ = s.rawCmd("SELECT", fmt.Sprintf("%d", s.db))
	}
	return nil
}

func (s *Store) rawCmd(args ...string) error {
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, a := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(a), a)
	}
	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return err
	}
	buf := make([]byte, 4096)
	_, _ = s.conn.Read(buf)
	return nil
}

func (s *Store) set(ctx context.Context, key string, value any) error {
	data, _ := json.Marshal(value)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rawCmd("SET", key, string(data))
}

func (s *Store) get(_ context.Context, key string, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cmd := fmt.Sprintf("*2\r\n$3\r\nGET\r\n$%d\r\n%s\r\n", len(key), key)
	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return err
	}
	buf := make([]byte, 65536)
	n, _ := s.conn.Read(buf)
	resp := string(buf[:n])
	if len(resp) < 4 || resp[0] == '-' {
		return fmt.Errorf("redis: key not found: %s", key)
	}
	start := 0
	for i, c := range resp {
		if c == '{' || c == '[' {
			start = i
			break
		}
	}
	if start > 0 {
		return json.Unmarshal([]byte(resp[start:]), out)
	}
	return fmt.Errorf("redis: invalid response for key %s", key)
}

func (s *Store) del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rawCmd("DEL", key)
}

func sessionKey(id string) string    { return "chronos:session:" + id }
func memoryKey(id string) string     { return "chronos:memory:" + id }
func auditKey(id string) string      { return "chronos:audit:" + id }
func traceKey(id string) string      { return "chronos:trace:" + id }
func eventKey(id string) string      { return "chronos:event:" + id }
func checkpointKey(id string) string { return "chronos:checkpoint:" + id }

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	return s.set(ctx, sessionKey(sess.ID), sess)
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	var sess storage.Session
	if err := s.get(ctx, sessionKey(id), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	return s.set(ctx, sessionKey(sess.ID), sess)
}

func (s *Store) ListSessions(_ context.Context, _ string, _, _ int) ([]*storage.Session, error) {
	return []*storage.Session{}, nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	return s.set(ctx, memoryKey(m.ID), m)
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	var m storage.MemoryRecord
	id := fmt.Sprintf("mem_%s_lt_%s", agentID, key)
	if err := s.get(ctx, memoryKey(id), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMemory(_ context.Context, _ string, _ string) ([]*storage.MemoryRecord, error) {
	return []*storage.MemoryRecord{}, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	return s.del(ctx, memoryKey(id))
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	return s.set(ctx, auditKey(log.ID), log)
}

func (s *Store) ListAuditLogs(_ context.Context, _ string, _, _ int) ([]*storage.AuditLog, error) {
	return []*storage.AuditLog{}, nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	return s.set(ctx, traceKey(t.ID), t)
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.get(ctx, traceKey(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(_ context.Context, _ string) ([]*storage.Trace, error) {
	return []*storage.Trace{}, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	return s.set(ctx, eventKey(e.ID), e)
}

func (s *Store) ListEvents(_ context.Context, _ string, _ int64) ([]*storage.Event, error) {
	return []*storage.Event{}, nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	return s.set(ctx, checkpointKey(cp.ID), cp)
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.get(ctx, checkpointKey(id), &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(_ context.Context, sessionID string) (*storage.Checkpoint, error) {
	return nil, fmt.Errorf("redis: no checkpoint found for session %q", sessionID)
}

func (s *Store) ListCheckpoints(_ context.Context, _ string) ([]*storage.Checkpoint, error) {
	return []*storage.Checkpoint{}, nil
}

// --- Lifecycle ---

func (s *Store) Migrate(_ context.Context) error { return nil }

func (s *Store) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}
