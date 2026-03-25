package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// --- Migrate: fail Nth Exec (CREATE TABLE loop) ---

var deepMigrateFailAfterN int

type deepMigrateFailDriver struct{}

func (d *deepMigrateFailDriver) Open(name string) (driver.Conn, error) {
	return &deepMigrateFailConn{n: 0}, nil
}

type deepMigrateFailConn struct{ n int }

func (c *deepMigrateFailConn) Prepare(query string) (driver.Stmt, error) {
	return &deepMigrateFailStmt{c: c}, nil
}
func (c *deepMigrateFailConn) Close() error              { return nil }
func (c *deepMigrateFailConn) Begin() (driver.Tx, error) { return deepNoRowTx{}, nil }

type deepMigrateFailStmt struct{ c *deepMigrateFailConn }

func (s *deepMigrateFailStmt) Close() error  { return nil }
func (s *deepMigrateFailStmt) NumInput() int { return -1 }

func (s *deepMigrateFailStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.c.n++
	if deepMigrateFailAfterN > 0 && s.c.n >= deepMigrateFailAfterN {
		return nil, errors.New("deep: migrate exec stopped")
	}
	return deepOKResult{}, nil
}

func (s *deepMigrateFailStmt) Query(args []driver.Value) (driver.Rows, error) {
	return deepEmptyRows{}, nil
}

type deepOKResult struct{}

func (deepOKResult) LastInsertId() (int64, error) { return 0, nil }
func (deepOKResult) RowsAffected() (int64, error) { return 1, nil }

type deepEmptyRows struct{}

func (deepEmptyRows) Columns() []string              { return []string{"x"} }
func (deepEmptyRows) Close() error                   { return nil }
func (deepEmptyRows) Next(dest []driver.Value) error { return io.EOF }

type deepNoRowTx struct{}

func (deepNoRowTx) Commit() error   { return nil }
func (deepNoRowTx) Rollback() error { return nil }

func init() {
	sql.Register("postgres_deep_migrate_fail", &deepMigrateFailDriver{})
}

func TestMigrate_ExecFailure_Deep(t *testing.T) {
	deepMigrateFailAfterN = 2
	t.Cleanup(func() { deepMigrateFailAfterN = 0 })

	db, err := sql.Open("postgres_deep_migrate_fail", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	err = s.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected migrate error")
	}
}

// --- GetMemory: zero rows ---

type deepNoRowMemoryDriver struct{}

func (d *deepNoRowMemoryDriver) Open(name string) (driver.Conn, error) {
	return deepNoRowMemoryConn{}, nil
}

type deepNoRowMemoryConn struct{}

func (c deepNoRowMemoryConn) Prepare(query string) (driver.Stmt, error) {
	return deepNoRowMemoryStmt{query: query}, nil
}
func (c deepNoRowMemoryConn) Close() error              { return nil }
func (c deepNoRowMemoryConn) Begin() (driver.Tx, error) { return deepNoRowTx{}, nil }

type deepNoRowMemoryStmt struct{ query string }

func (s deepNoRowMemoryStmt) Close() error  { return nil }
func (s deepNoRowMemoryStmt) NumInput() int { return -1 }

func (s deepNoRowMemoryStmt) Exec(args []driver.Value) (driver.Result, error) {
	return deepOKResult{}, nil
}

func (s deepNoRowMemoryStmt) Query(args []driver.Value) (driver.Rows, error) {
	if containsAll(s.query, "memory", "agent_id") && containsAll(s.query, "WHERE") {
		return deepEmptyRows{}, nil
	}
	return newPGMockRows(s.query), nil
}

func init() {
	sql.Register("postgres_deep_memory_norow", &deepNoRowMemoryDriver{})
}

func TestGetMemory_NoRow_Deep(t *testing.T) {
	db, err := sql.Open("postgres_deep_memory_norow", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	_, err = s.GetMemory(context.Background(), "agent-1", "missing")
	if err == nil {
		t.Fatal("expected ErrNoRows")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

// --- ListSessions: iteration error on second row ---

type deepSessIterErrDriver struct{}

func (d *deepSessIterErrDriver) Open(name string) (driver.Conn, error) {
	return deepSessIterErrConn{}, nil
}

type deepSessIterErrConn struct{}

func (c deepSessIterErrConn) Prepare(query string) (driver.Stmt, error) {
	return deepSessIterErrStmt{q: query}, nil
}
func (c deepSessIterErrConn) Close() error              { return nil }
func (c deepSessIterErrConn) Begin() (driver.Tx, error) { return deepNoRowTx{}, nil }

type deepSessIterErrStmt struct{ q string }

func (s deepSessIterErrStmt) Close() error  { return nil }
func (s deepSessIterErrStmt) NumInput() int { return -1 }
func (s deepSessIterErrStmt) Exec(args []driver.Value) (driver.Result, error) {
	return deepOKResult{}, nil
}
func (s deepSessIterErrStmt) Query(args []driver.Value) (driver.Rows, error) {
	if containsAll(s.q, "sessions", "agent_id") && containsAll(s.q, "ORDER BY") {
		return &deepSessIterRows{}, nil
	}
	return newPGMockRows(s.q), nil
}

type deepSessIterRows struct {
	row int
}

func (r *deepSessIterRows) Columns() []string {
	return []string{"id", "agent_id", "status", "metadata", "created_at", "updated_at"}
}
func (r *deepSessIterRows) Close() error { return nil }
func (r *deepSessIterRows) Next(dest []driver.Value) error {
	r.row++
	if r.row == 1 {
		now := time.Now()
		dest[0] = "s1"
		dest[1] = "a1"
		dest[2] = "running"
		dest[3] = []byte(`{}`)
		dest[4] = now
		dest[5] = now
		return nil
	}
	return errors.New("deep: session row iteration failed")
}

func init() {
	sql.Register("postgres_deep_sessions_iter_err", &deepSessIterErrDriver{})
}

func TestListSessions_IterationError_Deep(t *testing.T) {
	db, err := sql.Open("postgres_deep_sessions_iter_err", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	_, err = s.ListSessions(context.Background(), "agent-1", 10, 0)
	if err == nil {
		t.Fatal("expected rows error")
	}
}

// --- ListMemory: Query error ---

type deepListMemQueryErrDriver struct{}

func (d *deepListMemQueryErrDriver) Open(name string) (driver.Conn, error) {
	return deepListMemQueryErrConn{}, nil
}

type deepListMemQueryErrConn struct{}

func (c deepListMemQueryErrConn) Prepare(query string) (driver.Stmt, error) {
	return deepListMemQueryErrStmt{q: query}, nil
}
func (c deepListMemQueryErrConn) Close() error              { return nil }
func (c deepListMemQueryErrConn) Begin() (driver.Tx, error) { return deepNoRowTx{}, nil }

type deepListMemQueryErrStmt struct{ q string }

func (s deepListMemQueryErrStmt) Close() error  { return nil }
func (s deepListMemQueryErrStmt) NumInput() int { return -1 }
func (s deepListMemQueryErrStmt) Exec(args []driver.Value) (driver.Result, error) {
	return deepOKResult{}, nil
}
func (s deepListMemQueryErrStmt) Query(args []driver.Value) (driver.Rows, error) {
	if containsAll(s.q, "memory", "agent_id") && containsAll(s.q, "kind") {
		return nil, errors.New("deep: list memory query failed")
	}
	return newPGMockRows(s.q), nil
}

func init() {
	sql.Register("postgres_deep_listmem_qerr", &deepListMemQueryErrDriver{})
}

func TestListMemory_QueryError_Deep(t *testing.T) {
	db, err := sql.Open("postgres_deep_listmem_qerr", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	_, err = s.ListMemory(context.Background(), "agent-1", "long_term")
	if err == nil {
		t.Fatal("expected query error")
	}
}

// --- AppendEvent: Exec error ---

type deepEventExecErrDriver struct{}

func (d *deepEventExecErrDriver) Open(name string) (driver.Conn, error) {
	return deepEventExecErrConn{}, nil
}

type deepEventExecErrConn struct{}

func (c deepEventExecErrConn) Prepare(query string) (driver.Stmt, error) {
	return deepEventExecErrStmt{q: query}, nil
}
func (c deepEventExecErrConn) Close() error              { return nil }
func (c deepEventExecErrConn) Begin() (driver.Tx, error) { return deepNoRowTx{}, nil }

type deepEventExecErrStmt struct{ q string }

func (s deepEventExecErrStmt) Close() error  { return nil }
func (s deepEventExecErrStmt) NumInput() int { return -1 }
func (s deepEventExecErrStmt) Query(args []driver.Value) (driver.Rows, error) {
	return newPGMockRows(s.q), nil
}
func (s deepEventExecErrStmt) Exec(args []driver.Value) (driver.Result, error) {
	if containsAll(s.q, "events") && containsAll(s.q, "INSERT") {
		return nil, errors.New("deep: insert event failed")
	}
	return deepOKResult{}, nil
}

func init() {
	sql.Register("postgres_deep_event_exec_err", &deepEventExecErrDriver{})
}

func TestAppendEvent_ExecError_Deep(t *testing.T) {
	db, err := sql.Open("postgres_deep_event_exec_err", "x")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Store{db: db}
	e := &storage.Event{
		ID: "e1", SessionID: "s", SeqNum: 1, Type: "t", Payload: map[string]any{"a": 1},
	}
	err = s.AppendEvent(context.Background(), e)
	if err == nil {
		t.Fatal("expected append event error")
	}
}
