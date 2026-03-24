package migrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

const (
	migratePushBeginFailDriver        = "migrate_push_begin_fail"
	migratePushVersionQueryFailDriver = "migrate_push_version_query_fail"
)

func init() {
	sql.Register(migratePushBeginFailDriver, &beginFailDriver{})
	sql.Register(migratePushVersionQueryFailDriver, &versionQueryFailDriver{})
}

type beginFailDriver struct{}

func (d *beginFailDriver) Open(string) (driver.Conn, error) {
	return &beginFailConn{}, nil
}

type beginFailConn struct{}

func (c *beginFailConn) Prepare(query string) (driver.Stmt, error) {
	return &beginFailStmt{query: query}, nil
}
func (c *beginFailConn) Close() error { return nil }
func (c *beginFailConn) Begin() (driver.Tx, error) {
	return nil, errors.New("begin transaction failed (test driver)")
}

type beginFailStmt struct {
	query string
}

func (s *beginFailStmt) Close() error  { return nil }
func (s *beginFailStmt) NumInput() int { return -1 }

func (s *beginFailStmt) Exec([]driver.Value) (driver.Result, error) {
	return beginOKResult{}, nil
}

func (s *beginFailStmt) Query([]driver.Value) (driver.Rows, error) {
	if strings.Contains(s.query, "COALESCE(MAX(version)") {
		return &int64Row{val: 0}, nil
	}
	return &emptyQueryRows{}, nil
}

type beginOKResult struct{}

func (beginOKResult) LastInsertId() (int64, error) { return 0, nil }
func (beginOKResult) RowsAffected() (int64, error) { return 1, nil }

type int64Row struct {
	val  int64
	done bool
}

func (r *int64Row) Columns() []string { return []string{"version"} }
func (r *int64Row) Close() error      { return nil }
func (r *int64Row) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.val
	return nil
}

type emptyQueryRows struct{}

func (r *emptyQueryRows) Columns() []string { return nil }
func (r *emptyQueryRows) Close() error      { return nil }
func (r *emptyQueryRows) Next([]driver.Value) error {
	return io.EOF
}

func TestMigrate_Apply_BeginTxFails_Push(t *testing.T) {
	db, err := sql.Open(migratePushBeginFailDriver, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	m := New(db).Add(1, "first", "CREATE TABLE t_push (id INTEGER)", "DROP TABLE t_push")
	err = m.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected Migrate error when BeginTx fails")
	}
	if !strings.Contains(err.Error(), "begin tx") && !strings.Contains(err.Error(), "migrate v1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Driver: Exec succeeds, version Query fails (currentVersion path) ---

type versionQueryFailDriver struct{}

func (d *versionQueryFailDriver) Open(string) (driver.Conn, error) {
	return &versionQueryFailConn{}, nil
}

type versionQueryFailConn struct{}

func (c *versionQueryFailConn) Prepare(query string) (driver.Stmt, error) {
	return &versionQueryFailStmt{query: query}, nil
}
func (c *versionQueryFailConn) Close() error              { return nil }
func (c *versionQueryFailConn) Begin() (driver.Tx, error) { return pushOKTx{}, nil }

type versionQueryFailStmt struct {
	query string
}

func (s *versionQueryFailStmt) Close() error  { return nil }
func (s *versionQueryFailStmt) NumInput() int { return -1 }
func (s *versionQueryFailStmt) Exec([]driver.Value) (driver.Result, error) {
	return beginOKResult{}, nil
}
func (s *versionQueryFailStmt) Query([]driver.Value) (driver.Rows, error) {
	if strings.Contains(s.query, "COALESCE(MAX(version)") {
		return nil, errors.New("forced version query failure")
	}
	return &emptyQueryRows{}, nil
}

type pushOKTx struct{}

func (pushOKTx) Commit() error   { return nil }
func (pushOKTx) Rollback() error { return nil }

func TestMigrate_CurrentVersion_QueryError_Push(t *testing.T) {
	db, err := sql.Open(migratePushVersionQueryFailDriver, "")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	m := New(db).Add(1, "v", "CREATE TABLE t_v (id INTEGER)", "")
	err = m.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected error from currentVersion")
	}
	if !strings.Contains(err.Error(), "current version") {
		t.Fatalf("unexpected error: %v", err)
	}
}
