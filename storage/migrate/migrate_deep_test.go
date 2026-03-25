package migrate

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestRollback_NoMigrationsApplied_Deep(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "t", "CREATE TABLE t (id INTEGER)", "DROP TABLE t")

	err := m.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected rollback error when nothing applied")
	}
}

func TestRollback_MigrationNotInRegistry_Deep(t *testing.T) {
	db := testDB(t)
	_, err := db.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS _migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO _migrations (version, description) VALUES (99, 'orphan')`)
	if err != nil {
		t.Fatal(err)
	}

	m := New(db)
	m.Add(1, "only", "CREATE TABLE t (id INTEGER)", "DROP TABLE t")

	err = m.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected migration not found error")
	}
}

func TestRollback_NoDownSQL_Deep(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "no down", "CREATE TABLE t (id INTEGER)", "")

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	err := m.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected no rollback SQL error")
	}
}
