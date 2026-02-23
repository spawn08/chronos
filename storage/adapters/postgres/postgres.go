// Package postgres provides a PostgreSQL-backed Storage adapter for Chronos.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Store implements storage.Storage using PostgreSQL.
type Store struct {
	db *sql.DB
}

// New opens a PostgreSQL connection with the given DSN.
func New(dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	return &Store{db: db}, nil
}

// Migrate creates all required tables.
func (s *Store) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'running',
			metadata JSONB,
			created_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS memory (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			agent_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			key TEXT NOT NULL,
			value JSONB,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			detail JSONB,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS traces (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			parent_id TEXT,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			input JSONB,
			output JSONB,
			error TEXT,
			started_at TIMESTAMPTZ NOT NULL,
			ended_at TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			seq_num BIGINT NOT NULL,
			type TEXT NOT NULL,
			payload JSONB,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS checkpoints (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			run_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			state JSONB,
			seq_num BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq_num)`,
		`CREATE INDEX IF NOT EXISTS idx_checkpoints_session ON checkpoints(session_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_agent_key ON memory(agent_id, key)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- Sessions ---

func (s *Store) CreateSession(ctx context.Context, sess *storage.Session) error {
	meta, _ := json.Marshal(sess.Metadata)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, agent_id, status, metadata, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		sess.ID, sess.AgentID, sess.Status, meta, sess.CreatedAt, sess.UpdatedAt,
	)
	return err
}

func (s *Store) GetSession(ctx context.Context, id string) (*storage.Session, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, agent_id, status, metadata, created_at, updated_at FROM sessions WHERE id=$1`, id)
	sess := &storage.Session{}
	var meta []byte
	if err := row.Scan(&sess.ID, &sess.AgentID, &sess.Status, &meta, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(meta, &sess.Metadata)
	return sess, nil
}

func (s *Store) UpdateSession(ctx context.Context, sess *storage.Session) error {
	meta, _ := json.Marshal(sess.Metadata)
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status=$1, metadata=$2, updated_at=$3 WHERE id=$4`,
		sess.Status, meta, time.Now(), sess.ID,
	)
	return err
}

func (s *Store) ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*storage.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, status, metadata, created_at, updated_at FROM sessions WHERE agent_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		agentID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Session
	for rows.Next() {
		sess := &storage.Session{}
		var meta []byte
		if err := rows.Scan(&sess.ID, &sess.AgentID, &sess.Status, &meta, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &sess.Metadata)
		out = append(out, sess)
	}
	return out, rows.Err()
}

// --- Memory ---

func (s *Store) PutMemory(ctx context.Context, m *storage.MemoryRecord) error {
	val, _ := json.Marshal(m.Value)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory (id, session_id, agent_id, kind, key, value, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (id) DO UPDATE SET value=$6`,
		m.ID, m.SessionID, m.AgentID, m.Kind, m.Key, val, m.CreatedAt,
	)
	return err
}

func (s *Store) GetMemory(ctx context.Context, agentID, key string) (*storage.MemoryRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, agent_id, kind, key, value, created_at FROM memory WHERE agent_id=$1 AND key=$2`, agentID, key)
	m := &storage.MemoryRecord{}
	var val []byte
	if err := row.Scan(&m.ID, &m.SessionID, &m.AgentID, &m.Kind, &m.Key, &val, &m.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(val, &m.Value)
	return m, nil
}

func (s *Store) ListMemory(ctx context.Context, agentID, kind string) ([]*storage.MemoryRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, agent_id, kind, key, value, created_at FROM memory WHERE agent_id=$1 AND kind=$2`,
		agentID, kind,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.MemoryRecord
	for rows.Next() {
		m := &storage.MemoryRecord{}
		var val []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.AgentID, &m.Kind, &m.Key, &val, &m.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(val, &m.Value)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory WHERE id=$1`, id)
	return err
}

// --- Audit Logs ---

func (s *Store) AppendAuditLog(ctx context.Context, log *storage.AuditLog) error {
	detail, _ := json.Marshal(log.Detail)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_logs (id, session_id, actor, action, resource, detail, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		log.ID, log.SessionID, log.Actor, log.Action, log.Resource, detail, log.CreatedAt,
	)
	return err
}

func (s *Store) ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*storage.AuditLog, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, actor, action, resource, detail, created_at FROM audit_logs WHERE session_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		sessionID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.AuditLog
	for rows.Next() {
		l := &storage.AuditLog{}
		var detail []byte
		if err := rows.Scan(&l.ID, &l.SessionID, &l.Actor, &l.Action, &l.Resource, &detail, &l.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detail, &l.Detail)
		out = append(out, l)
	}
	return out, rows.Err()
}

// --- Traces ---

func (s *Store) InsertTrace(ctx context.Context, t *storage.Trace) error {
	inp, _ := json.Marshal(t.Input)
	outp, _ := json.Marshal(t.Output)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO traces (id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID, t.SessionID, t.ParentID, t.Name, t.Kind, inp, outp, t.Error, t.StartedAt, t.EndedAt,
	)
	return err
}

func (s *Store) GetTrace(ctx context.Context, id string) (*storage.Trace, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at FROM traces WHERE id=$1`, id)
	t := &storage.Trace{}
	var inp, outp []byte
	if err := row.Scan(&t.ID, &t.SessionID, &t.ParentID, &t.Name, &t.Kind, &inp, &outp, &t.Error, &t.StartedAt, &t.EndedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(inp, &t.Input)
	_ = json.Unmarshal(outp, &t.Output)
	return t, nil
}

func (s *Store) ListTraces(ctx context.Context, sessionID string) ([]*storage.Trace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at FROM traces WHERE session_id=$1 ORDER BY started_at`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Trace
	for rows.Next() {
		t := &storage.Trace{}
		var inp, outp []byte
		if err := rows.Scan(&t.ID, &t.SessionID, &t.ParentID, &t.Name, &t.Kind, &inp, &outp, &t.Error, &t.StartedAt, &t.EndedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(inp, &t.Input)
		_ = json.Unmarshal(outp, &t.Output)
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- Events ---

func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	payload, _ := json.Marshal(e.Payload)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, session_id, seq_num, type, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		e.ID, e.SessionID, e.SeqNum, e.Type, payload, e.CreatedAt,
	)
	return err
}

func (s *Store) ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*storage.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq_num, type, payload, created_at FROM events WHERE session_id=$1 AND seq_num>$2 ORDER BY seq_num`,
		sessionID, afterSeq,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Event
	for rows.Next() {
		e := &storage.Event{}
		var payload []byte
		if err := rows.Scan(&e.ID, &e.SessionID, &e.SeqNum, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &e.Payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// --- Checkpoints ---

func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	state, _ := json.Marshal(cp.State)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, run_id, node_id, state, seq_num, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		cp.ID, cp.SessionID, cp.RunID, cp.NodeID, state, cp.SeqNum, cp.CreatedAt,
	)
	return err
}

func (s *Store) GetCheckpoint(ctx context.Context, id string) (*storage.Checkpoint, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE id=$1`, id)
	cp := &storage.Checkpoint{}
	var state []byte
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.RunID, &cp.NodeID, &state, &cp.SeqNum, &cp.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(state, &cp.State)
	return cp, nil
}

func (s *Store) GetLatestCheckpoint(ctx context.Context, sessionID string) (*storage.Checkpoint, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE session_id=$1 ORDER BY created_at DESC LIMIT 1`,
		sessionID,
	)
	cp := &storage.Checkpoint{}
	var state []byte
	if err := row.Scan(&cp.ID, &cp.SessionID, &cp.RunID, &cp.NodeID, &state, &cp.SeqNum, &cp.CreatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(state, &cp.State)
	return cp, nil
}

func (s *Store) ListCheckpoints(ctx context.Context, sessionID string) ([]*storage.Checkpoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE session_id=$1 ORDER BY seq_num`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Checkpoint
	for rows.Next() {
		cp := &storage.Checkpoint{}
		var state []byte
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.RunID, &cp.NodeID, &state, &cp.SeqNum, &cp.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(state, &cp.State)
		out = append(out, cp)
	}
	return out, rows.Err()
}
