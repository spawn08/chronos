package migrate

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMigrate_OutOfOrderVersions_AppliesSorted(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(3, "third", "CREATE TABLE t3 (id INTEGER)", "DROP TABLE t3")
	m.Add(1, "first", "CREATE TABLE t1 (id INTEGER)", "DROP TABLE t1")
	m.Add(2, "second", "CREATE TABLE t2 (id INTEGER)", "DROP TABLE t2")

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	for _, tbl := range []string{"t1", "t2", "t3"} {
		if _, err := db.Exec("INSERT INTO " + tbl + " (id) VALUES (1)"); err != nil {
			t.Fatalf("insert into %s: %v", tbl, err)
		}
	}

	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.CurrentVersion != 3 {
		t.Errorf("CurrentVersion = %d, want 3", st.CurrentVersion)
	}
}

func TestMigrate_ClosedDB(t *testing.T) {
	db := testDB(t)
	m := New(db)
	m.Add(1, "v1", "CREATE TABLE x (id INTEGER)", "DROP TABLE x")
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := m.Migrate(context.Background()); err == nil {
		t.Fatal("expected error when DB is closed")
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("expected Status error when DB is closed")
	}
}

func TestRollback_ClosedDB(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	m := New(db)
	m.Add(1, "v1", "CREATE TABLE y (id INTEGER)", "DROP TABLE y")
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := m.Rollback(context.Background()); err == nil {
		t.Fatal("expected error when DB is closed")
	}
}

func TestStatus_ClosedDB(t *testing.T) {
	db := testDB(t)
	m := New(db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("expected error when DB is closed")
	}
}
