package migrate

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrate_Basic(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "create users", "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)", "DROP TABLE users")
	m.Add(2, "add email", "ALTER TABLE users ADD COLUMN email TEXT", "")

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Verify table exists
	_, err := db.Exec("INSERT INTO users (name, email) VALUES ('test', 'test@test.com')")
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "create table", "CREATE TABLE t (id INTEGER)", "DROP TABLE t")

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Run again — should be no-op
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestMigrate_Status(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "first", "CREATE TABLE t1 (id INTEGER)", "")
	m.Add(2, "second", "CREATE TABLE t2 (id INTEGER)", "")

	m.Migrate(context.Background())

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.CurrentVersion != 2 {
		t.Errorf("version = %d, want 2", status.CurrentVersion)
	}
	if len(status.Applied) != 2 {
		t.Errorf("applied = %d, want 2", len(status.Applied))
	}
	if len(status.Pending) != 0 {
		t.Errorf("pending = %d, want 0", len(status.Pending))
	}
}

func TestMigrate_Rollback(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "create", "CREATE TABLE t (id INTEGER)", "DROP TABLE t")

	m.Migrate(context.Background())

	if err := m.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Table should be gone
	_, err := db.Exec("INSERT INTO t (id) VALUES (1)")
	if err == nil {
		t.Fatal("expected error after rollback")
	}
}

func TestMigrate_RollbackEmpty(t *testing.T) {
	db := testDB(t)
	m := New(db)

	err := m.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected error for empty rollback")
	}
}

func TestMigrate_Pending(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "first", "CREATE TABLE t1 (id INTEGER)", "")
	m.Add(2, "second", "CREATE TABLE t2 (id INTEGER)", "")

	// Apply only first
	m2 := New(db)
	m2.Add(1, "first", "CREATE TABLE t1 (id INTEGER)", "")
	m2.Migrate(context.Background())

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Pending) != 1 {
		t.Errorf("pending = %d, want 1", len(status.Pending))
	}
	if status.Pending[0].Version != 2 {
		t.Errorf("pending version = %d, want 2", status.Pending[0].Version)
	}
}

func TestMigrate_Rollback_NoDownSQL(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "create", "CREATE TABLE t (id INTEGER)", "") // no Down SQL

	m.Migrate(context.Background())

	err := m.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected error for missing Down SQL")
	}
}

func TestMigrate_Rollback_MigrationNotInRegistry(t *testing.T) {
	db := testDB(t)
	// Migrate with v1
	m1 := New(db)
	m1.Add(1, "create", "CREATE TABLE t (id INTEGER)", "DROP TABLE t")
	m1.Migrate(context.Background())

	// Create a migrator with no matching version
	m2 := New(db)
	m2.Add(2, "other", "CREATE TABLE t2 (id INTEGER)", "DROP TABLE t2")

	err := m2.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected error for migration not in registry")
	}
}

func TestMigrate_Apply_BadSQL(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "bad", "NOT VALID SQL ;;;", "")

	err := m.Migrate(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid SQL")
	}
}

func TestMigrate_Status_Empty(t *testing.T) {
	db := testDB(t)
	m := New(db)

	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.CurrentVersion != 0 {
		t.Errorf("expected version 0, got %d", status.CurrentVersion)
	}
	if len(status.Applied) != 0 {
		t.Errorf("expected 0 applied, got %d", len(status.Applied))
	}
}

func TestMigrate_MultiStep_Rollback(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "first", "CREATE TABLE t1 (id INTEGER)", "DROP TABLE t1")
	m.Add(2, "second", "CREATE TABLE t2 (id INTEGER)", "DROP TABLE t2")
	m.Migrate(context.Background())

	// Rollback should remove v2
	if err := m.Rollback(context.Background()); err != nil {
		t.Fatalf("Rollback v2: %v", err)
	}
	status, _ := m.Status(context.Background())
	if status.CurrentVersion != 1 {
		t.Errorf("after rollback, want version 1, got %d", status.CurrentVersion)
	}
}
