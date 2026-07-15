package queue

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// sqliteDSN returns a shared, WAL-mode SQLite DSN under the test's temp dir.
// WAL + a busy timeout lets multiple independent *sql.DB handles (simulating
// distributed workers) contend on the same file without spurious lock errors.
func sqliteDSN(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "queue.db")
	return path + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate&_foreign_keys=on"
}

// openStore opens an independent SQLStore over the given DSN.
func openStore(t *testing.T, dsn string) *SQLStore {
	t.Helper()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLStore(db, DialectSQLite)
}

// newStore opens a migrated SQLStore on a fresh DSN.
func newStore(t *testing.T) *SQLStore {
	t.Helper()
	s := openStore(t, sqliteDSN(t))
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}
