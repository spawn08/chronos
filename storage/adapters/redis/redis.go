// Package redis provides a Redis-backed Storage adapter for Chronos.
// Uses the Redis RESP protocol via a minimal TCP client.
package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
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
		if _, err := s.rawCmdResp("AUTH", s.password); err != nil {
			return fmt.Errorf("redis auth: %w", err)
		}
	}
	if s.db != 0 {
		if _, err := s.rawCmdResp("SELECT", fmt.Sprintf("%d", s.db)); err != nil {
			return fmt.Errorf("redis select db: %w", err)
		}
	}
	return nil
}

// rawCmdResp sends a RESP command and returns the raw response bytes.
func (s *Store) rawCmdResp(args ...string) (string, error) {
	cmd := fmt.Sprintf("*%d\r\n", len(args))
	for _, a := range args {
		cmd += fmt.Sprintf("$%d\r\n%s\r\n", len(a), a)
	}
	_, err := s.conn.Write([]byte(cmd))
	if err != nil {
		return "", fmt.Errorf("redis write: %w", err)
	}
	buf := make([]byte, 65536)
	n, err := s.conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("redis read: %w", err)
	}
	resp := string(buf[:n])
	if resp != "" && resp[0] == '-' {
		return "", fmt.Errorf("redis error: %s", strings.TrimSpace(resp[1:]))
	}
	return resp, nil
}

func (s *Store) set(_ context.Context, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("redis marshal: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, cmdErr := s.rawCmdResp("SET", key, string(data))
	return cmdErr
}

func (s *Store) get(_ context.Context, key string, out any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, err := s.rawCmdResp("GET", key)
	if err != nil {
		return fmt.Errorf("redis get %s: %w", key, err)
	}
	jsonData := extractJSON(resp)
	if jsonData == "" {
		return fmt.Errorf("redis: key not found: %s", key)
	}
	if err := json.Unmarshal([]byte(jsonData), out); err != nil {
		return fmt.Errorf("redis unmarshal %s: %w", key, err)
	}
	return nil
}

func (s *Store) del(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.rawCmdResp("DEL", key)
	return err
}

// extractJSON finds the first JSON object or array in a RESP bulk string response.
func extractJSON(resp string) string {
	for i, c := range resp {
		if c != '{' && c != '[' {
			continue
		}
		depth := 0
		opener, closer := '{', '}'
		if c == '[' {
			opener, closer = '[', ']'
		}
		for j := i; j < len(resp); j++ {
			if rune(resp[j]) == opener {
				depth++
			} else if rune(resp[j]) == closer {
				depth--
				if depth == 0 {
					return resp[i : j+1]
				}
			}
		}
		return resp[i:]
	}
	return ""
}

// parseScanResponse extracts cursor and keys from a SCAN response.
func parseScanResponse(resp string) (cursor string, keys []string) {
	lines := strings.Split(resp, "\r\n")
	cursor = "0"
	for i, line := range lines {
		if line != "" && line[0] == '$' && i+1 < len(lines) {
			if cursor == "0" && !strings.HasPrefix(lines[i+1], "chronos:") {
				cursor = lines[i+1]
			} else if strings.HasPrefix(lines[i+1], "chronos:") {
				keys = append(keys, lines[i+1])
			}
		}
	}
	return cursor, keys
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

// addToIndex adds a member to a sorted set index with a score (timestamp).
func (s *Store) addToIndex(indexKey, member string, score float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.rawCmdResp("ZADD", indexKey, fmt.Sprintf("%f", score), member)
	return err
}

// getFromIndex returns members from a sorted set index.
func (s *Store) getFromIndex(indexKey string, limit, offset int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := fmt.Sprintf("%d", offset)
	stop := fmt.Sprintf("%d", offset+limit-1)
	resp, err := s.rawCmdResp("ZREVRANGE", indexKey, start, stop)
	if err != nil {
		return nil, fmt.Errorf("redis zrevrange: %w", err)
	}
	return parseArrayResponse(resp), nil
}

// getAllFromIndex returns all members from a sorted set index.
func (s *Store) getAllFromIndex(indexKey string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	resp, err := s.rawCmdResp("ZRANGE", indexKey, "0", "-1")
	if err != nil {
		return nil, fmt.Errorf("redis zrange: %w", err)
	}
	return parseArrayResponse(resp), nil
}

// parseArrayResponse extracts string values from a RESP array response.
func parseArrayResponse(resp string) []string {
	var result []string
	lines := strings.Split(resp, "\r\n")
	for i, line := range lines {
		if line != "" && line[0] == '$' && i+1 < len(lines) {
			val := lines[i+1]
			if val != "" {
				result = append(result, val)
			}
		}
	}
	return result
}

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	if err := s.set(ctx, sessionKey(sess.ID), sess); err != nil {
		return err
	}
	return s.addToIndex(sessionIndexKey(sess.AgentID), sess.ID, float64(sess.CreatedAt.UnixMilli()))
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

func (s *Store) ListSessions(_ context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := s.getFromIndex(sessionIndexKey(agentID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("redis list sessions: %w", err)
	}
	sessions := make([]*storage.Session, 0, len(ids))
	for _, id := range ids {
		var sess storage.Session
		if getErr := s.get(context.Background(), sessionKey(id), &sess); getErr == nil {
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
	return s.addToIndex(memoryIndexKey(m.AgentID, m.Kind), m.ID, float64(m.CreatedAt.UnixMilli()))
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	var m storage.MemoryRecord
	id := fmt.Sprintf("mem_%s_lt_%s", agentID, key)
	if err := s.get(ctx, memoryKey(id), &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListMemory(_ context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	ids, err := s.getAllFromIndex(memoryIndexKey(agentID, kind))
	if err != nil {
		return nil, fmt.Errorf("redis list memory: %w", err)
	}
	records := make([]*storage.MemoryRecord, 0, len(ids))
	for _, id := range ids {
		var m storage.MemoryRecord
		if getErr := s.get(context.Background(), memoryKey(id), &m); getErr == nil {
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
	return s.addToIndex(auditIndexKey(log.SessionID), log.ID, float64(log.CreatedAt.UnixMilli()))
}

func (s *Store) ListAuditLogs(_ context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	if limit <= 0 {
		limit = 100
	}
	ids, err := s.getFromIndex(auditIndexKey(sessionID), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("redis list audit logs: %w", err)
	}
	logs := make([]*storage.AuditLog, 0, len(ids))
	for _, id := range ids {
		var log storage.AuditLog
		if getErr := s.get(context.Background(), auditKey(id), &log); getErr == nil {
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
	return s.addToIndex(traceIndexKey(t.SessionID), t.ID, float64(t.StartedAt.UnixMilli()))
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	var t storage.Trace
	if err := s.get(ctx, traceKey(id), &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) ListTraces(_ context.Context, sessionID string) ([]*storage.Trace, error) {
	ids, err := s.getAllFromIndex(traceIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list traces: %w", err)
	}
	traces := make([]*storage.Trace, 0, len(ids))
	for _, id := range ids {
		var t storage.Trace
		if getErr := s.get(context.Background(), traceKey(id), &t); getErr == nil {
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
	return s.addToIndex(eventIndexKey(e.SessionID), e.ID, float64(e.SeqNum))
}

func (s *Store) ListEvents(_ context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	ids, err := s.getAllFromIndex(eventIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list events: %w", err)
	}
	events := make([]*storage.Event, 0, len(ids))
	for _, id := range ids {
		var e storage.Event
		if getErr := s.get(context.Background(), eventKey(id), &e); getErr == nil {
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
	return s.addToIndex(checkpointIndexKey(cp.SessionID), cp.ID, float64(cp.SeqNum))
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	var cp storage.Checkpoint
	if err := s.get(ctx, checkpointKey(id), &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func (s *Store) GetLatestCheckpoint(_ context.Context, sessionID string) (*storage.Checkpoint, error) {
	ids, err := s.getFromIndex(checkpointIndexKey(sessionID), 1, 0)
	if err != nil || len(ids) == 0 {
		return nil, fmt.Errorf("redis: no checkpoint found for session %q", sessionID)
	}
	var cp storage.Checkpoint
	if getErr := s.get(context.Background(), checkpointKey(ids[0]), &cp); getErr != nil {
		return nil, fmt.Errorf("redis get latest checkpoint: %w", getErr)
	}
	return &cp, nil
}

func (s *Store) ListCheckpoints(_ context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	ids, err := s.getAllFromIndex(checkpointIndexKey(sessionID))
	if err != nil {
		return nil, fmt.Errorf("redis list checkpoints: %w", err)
	}
	checkpoints := make([]*storage.Checkpoint, 0, len(ids))
	for _, id := range ids {
		var cp storage.Checkpoint
		if getErr := s.get(context.Background(), checkpointKey(id), &cp); getErr == nil {
			checkpoints = append(checkpoints, &cp)
		}
	}
	return checkpoints, nil
}

// --- Lifecycle ---

func (s *Store) Migrate(_ context.Context) error { return nil }

func (s *Store) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Ensure Store implements storage.Storage at compile time.
var _ storage.Storage = (*Store)(nil)
