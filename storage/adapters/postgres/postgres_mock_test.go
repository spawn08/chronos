package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// ---------------------------------------------------------------------------
// Mock SQL driver for postgres
// ---------------------------------------------------------------------------

const mockPGDriverName = "postgres_mock"

func init() {
	sql.Register(mockPGDriverName, &pgMockDriver{})
}

type pgMockDriver struct{}

func (d *pgMockDriver) Open(name string) (driver.Conn, error) {
	return &pgMockConn{}, nil
}

type pgMockConn struct{}

func (c *pgMockConn) Prepare(query string) (driver.Stmt, error) {
	return &pgMockStmt{query: query}, nil
}
func (c *pgMockConn) Close() error                 { return nil }
func (c *pgMockConn) Begin() (driver.Tx, error)    { return &pgMockTx{}, nil }
func (c *pgMockConn) Ping(_ context.Context) error { return nil }

type pgMockTx struct{}

func (t *pgMockTx) Commit() error   { return nil }
func (t *pgMockTx) Rollback() error { return nil }

type pgMockStmt struct{ query string }

func (s *pgMockStmt) Close() error                                    { return nil }
func (s *pgMockStmt) NumInput() int                                   { return -1 }
func (s *pgMockStmt) Exec(args []driver.Value) (driver.Result, error) { return &pgMockResult{}, nil }
func (s *pgMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	return newPGMockRows(s.query), nil
}

type pgMockResult struct{}

func (r *pgMockResult) LastInsertId() (int64, error) { return 0, nil }
func (r *pgMockResult) RowsAffected() (int64, error) { return 1, nil }

// pgMockRows returns one row for any SELECT query.
type pgMockRows struct {
	query string
	done  bool
	now   time.Time
}

func newPGMockRows(query string) *pgMockRows {
	return &pgMockRows{query: query, now: time.Now()}
}

func (r *pgMockRows) Close() error { return nil }

func (r *pgMockRows) Columns() []string {
	// Detect which table is being queried by keywords in the query.
	// We return appropriate columns for each table.
	switch {
	case containsAll(r.query, "sessions", "agent_id"):
		return []string{"id", "agent_id", "status", "metadata", "created_at", "updated_at"}
	case containsAll(r.query, "memory", "agent_id"):
		return []string{"id", "session_id", "agent_id", "kind", "key", "value", "created_at"}
	case containsAll(r.query, "audit_logs"):
		return []string{"id", "session_id", "actor", "action", "resource", "detail", "created_at"}
	case containsAll(r.query, "traces"):
		return []string{"id", "session_id", "parent_id", "name", "kind", "input", "output", "error", "started_at", "ended_at"}
	case containsAll(r.query, "events"):
		return []string{"id", "session_id", "seq_num", "type", "payload", "created_at"}
	case containsAll(r.query, "checkpoints"):
		return []string{"id", "session_id", "run_id", "node_id", "state", "seq_num", "created_at"}
	default:
		return []string{"id"}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (r *pgMockRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	now := r.now

	switch {
	case containsAll(r.query, "sessions", "agent_id"):
		dest[0] = "sess-1"
		dest[1] = "agent-1"
		dest[2] = "running"
		dest[3] = []byte(`{}`)
		dest[4] = now
		dest[5] = now
	case containsAll(r.query, "memory", "agent_id"):
		dest[0] = "mem-1"
		dest[1] = ""
		dest[2] = "agent-1"
		dest[3] = "long_term"
		dest[4] = "key1"
		dest[5] = []byte(`"value1"`)
		dest[6] = now
	case containsAll(r.query, "audit_logs"):
		dest[0] = "audit-1"
		dest[1] = "sess-1"
		dest[2] = "user"
		dest[3] = "chat"
		dest[4] = "agent"
		dest[5] = []byte(`{}`)
		dest[6] = now
	case containsAll(r.query, "traces"):
		dest[0] = "trace-1"
		dest[1] = "sess-1"
		dest[2] = ""
		dest[3] = "chat"
		dest[4] = "agent"
		dest[5] = []byte(`null`)
		dest[6] = []byte(`null`)
		dest[7] = ""
		dest[8] = now
		dest[9] = now
	case containsAll(r.query, "events"):
		dest[0] = "evt-1"
		dest[1] = "sess-1"
		dest[2] = int64(1)
		dest[3] = "node_enter"
		dest[4] = []byte(`{}`)
		dest[5] = now
	case containsAll(r.query, "checkpoints"):
		dest[0] = "cp-1"
		dest[1] = "sess-1"
		dest[2] = "run-1"
		dest[3] = "node-1"
		dest[4] = []byte(`{}`)
		dest[5] = int64(1)
		dest[6] = now
	default:
		if len(dest) > 0 {
			dest[0] = "mock-id"
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper to create a mock-backed Store
// ---------------------------------------------------------------------------

func newMockPGStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open(mockPGDriverName, "mock://")
	if err != nil {
		t.Fatalf("sql.Open mock: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Store{db: db}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestMigrate_Mock(t *testing.T) {
	s := newMockPGStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestClose_Mock(t *testing.T) {
	s := newMockPGStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCreateSession_Mock(t *testing.T) {
	s := newMockPGStore(t)
	now := time.Now()
	sess := &storage.Session{
		ID: "s1", AgentID: "a1", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.CreateSession(context.Background(), sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
}

func TestUpdateSession_Mock(t *testing.T) {
	s := newMockPGStore(t)
	now := time.Now()
	sess := &storage.Session{
		ID: "s1", AgentID: "a1", Status: "completed",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.UpdateSession(context.Background(), sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}
}

func TestGetSession_Mock(t *testing.T) {
	s := newMockPGStore(t)
	sess, err := s.GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.ID != "sess-1" {
		t.Errorf("ID = %q, want sess-1", sess.ID)
	}
}

func TestListSessions_Mock(t *testing.T) {
	s := newMockPGStore(t)
	sessions, err := s.ListSessions(context.Background(), "agent-1", 10, 0)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(sessions))
	}
}

func TestPutMemory_Mock(t *testing.T) {
	s := newMockPGStore(t)
	m := &storage.MemoryRecord{
		ID: "m1", AgentID: "a1", Kind: "long_term",
		Key: "key1", Value: "val", CreatedAt: time.Now(),
	}
	if err := s.PutMemory(context.Background(), m); err != nil {
		t.Fatalf("PutMemory: %v", err)
	}
}

func TestGetMemory_Mock(t *testing.T) {
	s := newMockPGStore(t)
	m, err := s.GetMemory(context.Background(), "agent-1", "key1")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if m.ID != "mem-1" {
		t.Errorf("ID = %q, want mem-1", m.ID)
	}
}

func TestListMemory_Mock(t *testing.T) {
	s := newMockPGStore(t)
	records, err := s.ListMemory(context.Background(), "agent-1", "long_term")
	if err != nil {
		t.Fatalf("ListMemory: %v", err)
	}
	if len(records) != 1 {
		t.Errorf("expected 1 record, got %d", len(records))
	}
}

func TestDeleteMemory_Mock(t *testing.T) {
	s := newMockPGStore(t)
	if err := s.DeleteMemory(context.Background(), "m1"); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
}

func TestAppendAuditLog_Mock(t *testing.T) {
	s := newMockPGStore(t)
	log := &storage.AuditLog{
		ID: "a1", SessionID: "sess-1", Actor: "user",
		Action: "chat", Resource: "agent", CreatedAt: time.Now(),
	}
	if err := s.AppendAuditLog(context.Background(), log); err != nil {
		t.Fatalf("AppendAuditLog: %v", err)
	}
}

func TestListAuditLogs_Mock(t *testing.T) {
	s := newMockPGStore(t)
	logs, err := s.ListAuditLogs(context.Background(), "sess-1", 10, 0)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("expected 1 audit log, got %d", len(logs))
	}
}

func TestInsertTrace_Mock(t *testing.T) {
	s := newMockPGStore(t)
	trace := &storage.Trace{
		ID: "t1", SessionID: "sess-1", Name: "chat",
		Kind: "agent", StartedAt: time.Now(),
	}
	if err := s.InsertTrace(context.Background(), trace); err != nil {
		t.Fatalf("InsertTrace: %v", err)
	}
}

func TestGetTrace_Mock(t *testing.T) {
	s := newMockPGStore(t)
	trace, err := s.GetTrace(context.Background(), "trace-1")
	if err != nil {
		t.Fatalf("GetTrace: %v", err)
	}
	if trace.ID != "trace-1" {
		t.Errorf("ID = %q, want trace-1", trace.ID)
	}
}

func TestListTraces_Mock(t *testing.T) {
	s := newMockPGStore(t)
	traces, err := s.ListTraces(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(traces))
	}
}

func TestAppendEvent_Mock(t *testing.T) {
	s := newMockPGStore(t)
	e := &storage.Event{
		ID: "e1", SessionID: "sess-1", SeqNum: 1,
		Type: "node_enter", Payload: map[string]any{"node": "start"},
	}
	if err := s.AppendEvent(context.Background(), e); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
}

func TestListEvents_Mock(t *testing.T) {
	s := newMockPGStore(t)
	events, err := s.ListEvents(context.Background(), "sess-1", 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestSaveCheckpoint_Mock(t *testing.T) {
	s := newMockPGStore(t)
	cp := &storage.Checkpoint{
		ID: "cp1", SessionID: "sess-1", RunID: "run-1",
		NodeID: "node-1", State: map[string]any{"x": 1}, SeqNum: 1,
		CreatedAt: time.Now(),
	}
	if err := s.SaveCheckpoint(context.Background(), cp); err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
}

func TestGetCheckpoint_Mock(t *testing.T) {
	s := newMockPGStore(t)
	cp, err := s.GetCheckpoint(context.Background(), "cp-1")
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if cp.ID != "cp-1" {
		t.Errorf("ID = %q, want cp-1", cp.ID)
	}
}

func TestGetLatestCheckpoint_Mock(t *testing.T) {
	s := newMockPGStore(t)
	cp, err := s.GetLatestCheckpoint(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetLatestCheckpoint: %v", err)
	}
	if cp.ID != "cp-1" {
		t.Errorf("ID = %q, want cp-1", cp.ID)
	}
}

func TestListCheckpoints_Mock(t *testing.T) {
	s := newMockPGStore(t)
	checkpoints, err := s.ListCheckpoints(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint, got %d", len(checkpoints))
	}
}

// TestStorageInterface verifies Store satisfies storage.Storage at compile time.
func TestStorageInterface(t *testing.T) {
	var _ storage.Storage = (*Store)(nil)
}
