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

const (
	mockPGExecFailDriver  = "postgres_iter6_exec_fail"
	mockPGQueryFailDriver = "postgres_iter6_query_fail"
)

func init() {
	sql.Register(mockPGExecFailDriver, &pgExecFailDriver{})
	sql.Register(mockPGQueryFailDriver, &pgQueryFailDriver{})
}

type pgExecFailDriver struct{}

func (d *pgExecFailDriver) Open(string) (driver.Conn, error) {
	return &pgExecFailConn{}, nil
}

type pgExecFailConn struct{}

func (c *pgExecFailConn) Prepare(query string) (driver.Stmt, error) {
	return &pgExecFailStmt{query: query}, nil
}
func (c *pgExecFailConn) Close() error              { return nil }
func (c *pgExecFailConn) Begin() (driver.Tx, error) { return &pgMockTx{}, nil }

type pgExecFailStmt struct{ query string }

func (s *pgExecFailStmt) Close() error  { return nil }
func (s *pgExecFailStmt) NumInput() int { return -1 }
func (s *pgExecFailStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("exec failed")
}
func (s *pgExecFailStmt) Query([]driver.Value) (driver.Rows, error) {
	return &pgNoRowRows{}, nil
}

// pgNoRowRows yields zero rows for QueryRow → sql.ErrNoRows on Scan.
type pgNoRowRows struct{}

func (r *pgNoRowRows) Columns() []string {
	return []string{"id", "agent_id", "status", "metadata", "created_at", "updated_at"}
}
func (r *pgNoRowRows) Close() error { return nil }
func (r *pgNoRowRows) Next([]driver.Value) error {
	return io.EOF
}

func newExecFailStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open(mockPGExecFailDriver, "mock://")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return &Store{db: db}
}

func TestMigrate_ExecError_ITER6(t *testing.T) {
	s := newExecFailStore(t)
	err := s.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected Migrate error")
	}
}

func TestGetSession_NoRows_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGExecFailDriver, "mock://")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Store{db: db}
	_, err = s.GetSession(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected ErrNoRows")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestCreateSession_ExecError_ITER6(t *testing.T) {
	s := newExecFailStore(t)
	now := time.Now()
	err := s.CreateSession(context.Background(), &storage.Session{
		ID: "s1", AgentID: "a1", Status: "running",
		CreatedAt: now, UpdatedAt: now,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestListSessions_QueryError_ITER6(t *testing.T) {
	db, err := sql.Open(mockPGQueryFailDriver, "mock://")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s := &Store{db: db}
	_, err = s.ListSessions(context.Background(), "a1", 10, 0)
	if err == nil {
		t.Fatal("expected query error")
	}
}

type pgQueryFailDriver struct{}

func (d *pgQueryFailDriver) Open(string) (driver.Conn, error) {
	return &pgQueryFailConn{}, nil
}

type pgQueryFailConn struct{}

func (c *pgQueryFailConn) Prepare(query string) (driver.Stmt, error) {
	return &pgQueryFailStmt{query: query}, nil
}
func (c *pgQueryFailConn) Close() error              { return nil }
func (c *pgQueryFailConn) Begin() (driver.Tx, error) { return &pgMockTx{}, nil }

type pgQueryFailStmt struct{ query string }

func (s *pgQueryFailStmt) Close() error  { return nil }
func (s *pgQueryFailStmt) NumInput() int { return -1 }
func (s *pgQueryFailStmt) Exec([]driver.Value) (driver.Result, error) {
	return &pgMockResult{}, nil
}
func (s *pgQueryFailStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("query failed")
}
