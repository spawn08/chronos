// Package redis provides a Redis-backed Storage adapter for Chronos.
//
// It is built on the official go-redis client (github.com/redis/go-redis/v9),
// which handles RESP2/RESP3 framing, connection pooling, pipelining, TLS and
// reconnection correctly. The previous hand-rolled TCP/RESP implementation
// assumed every reply fit in a single 64KB Read and parsed replies with fragile
// string scanning; both bugs are eliminated by delegating the wire protocol to
// the maintained driver.
//
// Data model: every record is stored as a JSON string under a namespaced key
// (chronos:<kind>:<id>). Secondary lookups (list-by-agent, list-by-session) are
// backed by per-owner sorted sets keyed by timestamp/sequence number so that
// listing is ordered and paginated without SCAN.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/redis/go-redis/v9"

	"github.com/spawn08/chronos/storage"
)

// Store implements storage.Storage using Redis.
type Store struct {
	client redis.UniversalClient
}

// New creates a Redis storage adapter and verifies connectivity.
func New(addr, password string, db int) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("redis connect: %w", err)
	}
	return &Store{client: client}, nil
}

// NewWithClient wraps an existing go-redis client. Useful for tests (miniredis)
// and for callers that need custom connection options (TLS, cluster, pooling).
func NewWithClient(client redis.UniversalClient) *Store {
	return &Store{client: client}
}

func (s *Store) set(ctx context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis marshal %s: %w", key, err)
	}
	if err := s.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

func (s *Store) get(ctx context.Context, key string, out any) error {
	data, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("redis: key not found: %s", key)
	}
	if err != nil {
		return fmt.Errorf("redis get %s: %w", key, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("redis unmarshal %s: %w", key, err)
	}
	return nil
}

func (s *Store) del(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis del %s: %w", key, err)
	}
	return nil
}

func sessionKey(id string) string    { return "chronos:session:" + id }
func memoryKey(id string) string     { return "chronos:memory:" + id }
func auditKey(id string) string      { return "chronos:audit:" + id }
func traceKey(id string) string      { return "chronos:trace:" + id }
func eventKey(id string) string      { return "chronos:event:" + id }
func checkpointKey(id string) string { return "chronos:checkpoint:" + id }

// sessionIndexKey stores a sorted set of session IDs per agent.
func sessionIndexKey(agentID string) string { return "chronos:idx:sessions:" + agentID }
func auditIndexKey(sessionID string) string { return "chronos:idx:audits:" + sessionID }
func traceIndexKey(sessionID string) string { return "chronos:idx:traces:" + sessionID }
func eventIndexKey(sessionID string) string { return "chronos:idx:events:" + sessionID }
func checkpointIndexKey(sessionID string) string {
	return "chronos:idx:checkpoints:" + sessionID
}
func memoryIndexKey(agentID, kind string) string {
	return "chronos:idx:memory:" + agentID + ":" + kind
}

// addToIndex adds a member to a sorted set index with a score (timestamp/seq).
func (s *Store) addToIndex(ctx context.Context, indexKey, member string, score float64) error {
	if err := s.client.ZAdd(ctx, indexKey, redis.Z{Score: score, Member: member}).Err(); err != nil {
		return fmt.Errorf("redis zadd %s: %w", indexKey, err)
	}
	return nil
}

// pageFromIndex returns a descending (newest-first) page of members.
func (s *Store) pageFromIndex(ctx context.Context, indexKey string, limit, offset int) ([]string, error) {
	start := int64(offset)
	stop := int64(offset + limit - 1)
	members, err := s.client.ZRevRange(ctx, indexKey, start, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("redis zrevrange %s: %w", indexKey, err)
	}
	return members, nil
}

// allFromIndex returns all members in ascending score order.
func (s *Store) allFromIndex(ctx context.Context, indexKey string) ([]string, error) {
	members, err := s.client.ZRange(ctx, indexKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("redis zrange %s: %w", indexKey, err)
	}
	return members, nil
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	if err := s.set(ctx, sessionKey(sess.ID), sess); err != nil {
		return err
	}
	return s.addToIndex(ctx, sessionIndexKey(sess.AgentID), sess.ID, float64(sess.CreatedAt.UnixMilli()))
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

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := s.pageFromIndex(ctx, sessionIndexKey(agentID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("redis list sessions: %w", err)
	}
	sessions := make([]*storage.Session, 0, len(ids))
	for _, id := range ids {
		var sess storage.Session
		if getErr := s.get(ctx, sessionKey(id), &sess); getErr == nil {
			sessions = append(sessions, &sess)
		}
	}
	return sessions, nil
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	if err := s.set(ctx, memoryKey(m.ID), m); err != nil {
		return err
	}
	return s.addToIndex(ctx, memoryIndexKey(m.AgentID, m.Kind), m.ID, float64(m.CreatedAt.UnixMilli()))
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	var m storage.MemoryRecord
	id := fmt.Sprintf("mem_%s_lt_%s", agentID, key)
	if err := s.get(ctx, memoryKey(id), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	ids, err := s.allFromIndex(ctx, memoryIndexKey(agentID, kind))
	if err != nil {
		return nil, fmt.Errorf("redis list memory: %w", err)
	}
	records := make([]*storage.MemoryRecord, 0, len(ids))
	for _, id := range ids {
		var m storage.MemoryRecord
		if getErr := s.get(ctx, memoryKey(id), &m); getErr == nil {
			records = append(records, &m)
		}
	}
	return records, nil
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	return s.del(ctx, memoryKey(id))
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	if err := s.set(ctx, auditKey(log.ID), log); err != nil {
		return err
	}
	return s.addToIndex(ctx, auditIndexKey(log.SessionID), log.ID, float64(log.CreatedAt.UnixMilli()))
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := s.pageFromIndex(ctx, auditIndexKey(sessionID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("redis list audit logs: %w", err)
	}
	logs := make([]*storage.AuditLog, 0, len(ids))
	for _, id := range ids {
		var log storage.AuditLog
		if getErr := s.get(ctx, auditKey(id), &log); getErr == nil {
			logs = append(logs, &log)
		}
	}
	return logs, nil
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	if err := s.set(ctx, traceKey(t.ID), t); err != nil {
		return err
	}
	return s.addToIndex(ctx, traceIndexKey(t.SessionID), t.ID, float64(t.StartedAt.UnixMilli()))
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.get(ctx, traceKey(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	ids, err := s.allFromIndex(ctx, traceIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list traces: %w", err)
	}
	traces := make([]*storage.Trace, 0, len(ids))
	for _, id := range ids {
		var t storage.Trace
		if getErr := s.get(ctx, traceKey(id), &t); getErr == nil {
			traces = append(traces, &t)
		}
	}
	return traces, nil
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	if err := s.set(ctx, eventKey(e.ID), e); err != nil {
		return err
	}
	return s.addToIndex(ctx, eventIndexKey(e.SessionID), e.ID, float64(e.SeqNum))
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	ids, err := s.allFromIndex(ctx, eventIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list events: %w", err)
	}
	events := make([]*storage.Event, 0, len(ids))
	for _, id := range ids {
		var e storage.Event
		if getErr := s.get(ctx, eventKey(id), &e); getErr == nil {
			if e.SeqNum > afterSeq {
				events = append(events, &e)
			}
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].SeqNum < events[j].SeqNum })
	return events, nil
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	if err := s.set(ctx, checkpointKey(cp.ID), cp); err != nil {
		return err
	}
	return s.addToIndex(ctx, checkpointIndexKey(cp.SessionID), cp.ID, float64(cp.SeqNum))
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.get(ctx, checkpointKey(id), &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	ids, err := s.pageFromIndex(ctx, checkpointIndexKey(sessionID), 1, 0)
	if err != nil {
		return nil, fmt.Errorf("redis get latest checkpoint: %w", err)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("redis: no checkpoint found for session %q", sessionID)
	}
	var cp storage.Checkpoint
	if getErr := s.get(ctx, checkpointKey(ids[0]), &cp); getErr != nil {
		return nil, fmt.Errorf("redis get latest checkpoint: %w", getErr)
	}
	return &cp, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	ids, err := s.allFromIndex(ctx, checkpointIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list checkpoints: %w", err)
	}
	checkpoints := make([]*storage.Checkpoint, 0, len(ids))
	for _, id := range ids {
		var cp storage.Checkpoint
		if getErr := s.get(ctx, checkpointKey(id), &cp); getErr == nil {
			checkpoints = append(checkpoints, &cp)
		}
	}
	return checkpoints, nil
}

// --- Lifecycle ---

// Migrate is a no-op for Redis; keyspaces are created lazily on first write.
func (s *Store) Migrate(_ context.Context) error { return nil }

func (s *Store) Close() error {
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			return fmt.Errorf("redis close: %w", err)
		}
	}
	return nil
}

// Ensure Store implements storage.Storage at compile time.
var _ storage.Storage = (*Store)(nil)
