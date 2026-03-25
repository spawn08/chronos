package migrate

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openSQLiteMax(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestMigrator_Rollback_NoMigrationsApplied(t *testing.T) {
	db := openSQLiteMax(t)
	m := New(db).Add(1, "one", `CREATE TABLE x (id INT)`, `DROP TABLE x`)
	ctx := context.Background()
	if err := m.Rollback(ctx); err == nil {
		t.Fatal("expected error when nothing to roll back")
	}
}

func TestMigrator_Rollback_MigrationNotInRegistry(t *testing.T) {
	db := openSQLiteMax(t)
	ctx := context.Background()
	m := New(db).Add(1, "one", `CREATE TABLE x (id INT)`, `DROP TABLE x`)
	if err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	m2 := New(db).Add(99, "other", `CREATE TABLE y (id INT)`, `DROP TABLE y`)
	if err := m2.Rollback(ctx); err == nil {
		t.Fatal("expected error: migration v1 not in m2 registry")
	}
}

func TestMigrator_Rollback_NoDownSQL(t *testing.T) {
	db := openSQLiteMax(t)
	ctx := context.Background()
	m := New(db).Add(1, "one", `CREATE TABLE x (id INT)`, ``)
	if err := m.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Rollback(ctx); err == nil {
		t.Fatal("expected error: no rollback SQL")
	}
}

func TestMigrator_apply_BeginTxError(t *testing.T) {
	db := openSQLiteMax(t)
	_ = db.Close()

	m := New(db).Add(1, "one", `CREATE TABLE x (id INT)`, `DROP TABLE x`)
	if err := m.Migrate(context.Background()); err == nil {
		t.Fatal("expected migrate error on closed db")
	}
}

func TestMigrator_Status_ScanErrorUsesClosedDB(t *testing.T) {
	db := openSQLiteMax(t)
	ctx := context.Background()
	m := New(db).Add(1, "a", `SELECT 1`, `SELECT 1`)
	_ = m.ensureTable(ctx)
	_ = db.Close()
	if _, err := m.Status(context.Background()); err == nil {
		t.Fatal("expected status error on closed db")
	}
}
