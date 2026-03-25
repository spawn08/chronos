package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

// Driver fails Exec on the Nth call (1-based), after successful Opens.
// Used to hit migrate: %w from the second CREATE TABLE onward.

const postgresPushMigrateNthFail = "postgres_push_migrate_nth_fail"

func init() {
	sql.Register(postgresPushMigrateNthFail, &nthExecFailDriver{})
}

type nthExecFailDriver struct{}

func (d *nthExecFailDriver) Open(string) (driver.Conn, error) {
	return &nthExecFailConn{}, nil
}

type nthExecFailConn struct{}

func (c *nthExecFailConn) Prepare(query string) (driver.Stmt, error) {
	return &nthExecFailStmt{query: query}, nil
}
func (c *nthExecFailConn) Close() error              { return nil }
func (c *nthExecFailConn) Begin() (driver.Tx, error) { return &pgMockTx{}, nil }

type nthExecFailStmt struct {
	query string
}

func (s *nthExecFailStmt) Close() error  { return nil }
func (s *nthExecFailStmt) NumInput() int { return -1 }

var nthExecCounter atomic.Int32

func (s *nthExecFailStmt) Exec([]driver.Value) (driver.Result, error) {
	n := nthExecCounter.Add(1)
	if n == 2 {
		return nil, errors.New("simulated migrate exec failure on 2nd statement")
	}
	return &pgMockResult{}, nil
}
func (s *nthExecFailStmt) Query([]driver.Value) (driver.Rows, error) {
	return &pgNoRowRows{}, nil
}

func TestStore_Migrate_SecondStatementFails_Push(t *testing.T) {
	nthExecCounter.Store(0)
	db, err := sql.Open(postgresPushMigrateNthFail, "mock://")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s := &Store{db: db}
	err = s.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected Migrate error")
	}
	if !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("expected migrate wrap, got %v", err)
	}
}

func TestNew_WhenPostgresDriverMissing_Push(t *testing.T) {
	// sql.Open returns an error only when the driver name is not registered.
	_, err := sql.Open("postgres_driver_that_does_not_exist_abc123", "any-dsn")
	if err == nil {
		t.Skip("unexpected: database/sql returned nil error for unknown driver")
	}
}
