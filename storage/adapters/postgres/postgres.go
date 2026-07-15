// Package postgres provides a PostgreSQL-backed Storage adapter for Chronos.
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver (P0-012)

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/migrate"
)

// Store implements storage.Storage using PostgreSQL.
type Store struct {
	db *sql.DB
}

// poolConfig holds configurable database/sql connection-pool settings, with
// production-sensible defaults.
type poolConfig struct {
	maxOpenConns    int
	maxIdleConns    int
	connMaxLifetime time.Duration
}

// Option configures a Store's connection pool.
type Option func(*poolConfig)

// WithMaxOpenConns sets the maximum number of open connections (default 25).
func WithMaxOpenConns(n int) Option {
	return func(c *poolConfig) { c.maxOpenConns = n }
}

// WithMaxIdleConns sets the maximum number of idle connections (default 5).
func WithMaxIdleConns(n int) Option {
	return func(c *poolConfig) { c.maxIdleConns = n }
}

// WithConnMaxLifetime sets the maximum lifetime of a connection (default 5m).
func WithConnMaxLifetime(d time.Duration) Option {
	return func(c *poolConfig) { c.connMaxLifetime = d }
}

// New opens a PostgreSQL connection with the given DSN using the pgx driver
// (registered via the blank import above).
func New(dsn string, opts ...Option) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	cfg := poolConfig{maxOpenConns: 25, maxIdleConns: 5, connMaxLifetime: 5 * time.Minute}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.maxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.maxOpenConns)
	}
	if cfg.maxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.maxIdleConns)
	}
	if cfg.connMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.connMaxLifetime)
	}
	return &Store{db: db}, nil
}

// schema is the v1 migration: all tables and indexes for the Postgres backend,
// including the P1-011 lookup indexes on sessions.agent_id, audit_logs.session_id
// and traces.session_id.
const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'running',
	metadata JSONB,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS memory (
	id TEXT PRIMARY KEY,
	session_id TEXT,
	agent_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	key TEXT NOT NULL,
	value JSONB,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	actor TEXT NOT NULL,
	action TEXT NOT NULL,
	resource TEXT NOT NULL,
	detail JSONB,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS traces (
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
);
CREATE TABLE IF NOT EXISTS events (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	seq_num BIGINT NOT NULL,
	type TEXT NOT NULL,
	payload JSONB,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS checkpoints (
	id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	run_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	state JSONB,
	seq_num BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq_num);
CREATE INDEX IF NOT EXISTS idx_checkpoints_session ON checkpoints(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_agent_key ON memory(agent_id, key);
CREATE UNIQUE INDEX IF NOT EXISTS uq_checkpoints_session_seq ON checkpoints(session_id, seq_num);
CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_audit_session ON audit_logs(session_id);
CREATE INDEX IF NOT EXISTS idx_traces_session ON traces(session_id, started_at);
`

// Migrate creates all required tables and indexes via the versioned migration
// framework, holding a pg_advisory_lock so concurrent migrators serialize
// (P1-014).
func (s *Store) Migrate(ctx context.Context) error {
	m := migrate.New(s.db, migrate.WithDialect(migrate.DialectPostgres))
	m.Add(1, "initial schema", schema, "")
	if err := m.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
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

// AppendEvent appends a ledger event. It is idempotent: re-appending an event
// with an existing id is a no-op, so replay/resume cannot error on the primary
// key or duplicate the ledger (P0-004).
func (s *Store) AppendEvent(ctx context.Context, e *storage.Event) error {
	payload, _ := json.Marshal(e.Payload)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id, session_id, seq_num, type, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (id) DO NOTHING`,
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

// SaveCheckpoint persists a checkpoint. It is idempotent: saving a checkpoint
// with an existing id overwrites it, so resuming/replaying a run cannot error on
// the primary key (P0-004).
func (s *Store) SaveCheckpoint(ctx context.Context, cp *storage.Checkpoint) error {
	state, _ := json.Marshal(cp.State)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, run_id, node_id, state, seq_num, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (id) DO UPDATE SET run_id=EXCLUDED.run_id, node_id=EXCLUDED.node_id, state=EXCLUDED.state, seq_num=EXCLUDED.seq_num, created_at=EXCLUDED.created_at`,
		cp.ID, cp.SessionID, cp.RunID, cp.NodeID, state, cp.SeqNum, cp.CreatedAt,
	)
	return err
}

// SaveCheckpointAndEvent persists a checkpoint and its ledger event atomically in
// a single transaction, satisfying graph.CheckpointCommitter. A crash cannot
// leave the checkpoint and event ledger out of sync (P0-004).
func (s *Store) SaveCheckpointAndEvent(ctx context.Context, cp *storage.Checkpoint, evt *storage.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	state, _ := json.Marshal(cp.State)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO checkpoints (id, session_id, run_id, node_id, state, seq_num, created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (id) DO UPDATE SET run_id=EXCLUDED.run_id, node_id=EXCLUDED.node_id, state=EXCLUDED.state, seq_num=EXCLUDED.seq_num, created_at=EXCLUDED.created_at`,
		cp.ID, cp.SessionID, cp.RunID, cp.NodeID, state, cp.SeqNum, cp.CreatedAt,
	); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}

	if evt != nil {
		payload, _ := json.Marshal(evt.Payload)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events (id, session_id, seq_num, type, payload, created_at) VALUES ($1,$2,$3,$4,$5,$6)
			 ON CONFLICT (id) DO NOTHING`,
			evt.ID, evt.SessionID, evt.SeqNum, evt.Type, payload, evt.CreatedAt,
		); err != nil {
			return fmt.Errorf("append event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit checkpoint and event: %w", err)
	}
	return nil
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
	// Order by seq_num (monotonic) rather than wall-clock: same-tick timestamps
	// would otherwise make the "latest" checkpoint non-deterministic (P0-003).
	row := s.db.QueryRowContext(ctx,
		`SELECT id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE session_id=$1 ORDER BY seq_num DESC LIMIT 1`,
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
