package builtins

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, email TEXT)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	_, err = db.Exec(`INSERT INTO users (name, email) VALUES ('Alice', 'alice@test.com'), ('Bob', 'bob@test.com')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	return db
}

func TestSQL_SelectAll(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, nil)

	result, err := sqlTool.Handler(context.Background(), map[string]any{
		"query": "SELECT name, email FROM users ORDER BY name",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["count"].(int) != 2 {
		t.Errorf("count = %v, want 2", m["count"])
	}
	rows := m["rows"].([]map[string]any)
	if rows[0]["name"] != "Alice" {
		t.Errorf("first row name = %v, want Alice", rows[0]["name"])
	}
}

func TestSQL_SelectWithParams(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, nil)

	result, err := sqlTool.Handler(context.Background(), map[string]any{
		"query":  "SELECT name FROM users WHERE email = ?",
		"params": []any{"bob@test.com"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["count"].(int) != 1 {
		t.Errorf("count = %v, want 1", m["count"])
	}
}

func TestSQL_InsertBlocked(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, nil) // default: SELECT only

	_, err := sqlTool.Handler(context.Background(), map[string]any{
		"query": "INSERT INTO users (name, email) VALUES ('Eve', 'eve@test.com')",
	})
	if err == nil {
		t.Fatal("expected error for blocked INSERT")
	}
}

func TestSQL_InsertAllowed(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, []string{"SELECT", "INSERT"})

	result, err := sqlTool.Handler(context.Background(), map[string]any{
		"query": "INSERT INTO users (name, email) VALUES ('Eve', 'eve@test.com')",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := result.(map[string]any)
	if m["rows_affected"].(int64) != 1 {
		t.Errorf("rows_affected = %v, want 1", m["rows_affected"])
	}
}

func TestSQL_EmptyQuery(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, nil)

	_, err := sqlTool.Handler(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestSQL_MissingQuery(t *testing.T) {
	db := setupTestDB(t)
	sqlTool := NewSQLTool(db, nil)

	_, err := sqlTool.Handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestSQL_Definition(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	sqlTool := NewSQLTool(db, nil)

	if sqlTool.Name != "sql_query" {
		t.Errorf("name = %q, want sql_query", sqlTool.Name)
	}
	if !sqlTool.RequiresConfirmation {
		t.Error("should require confirmation")
	}
}
