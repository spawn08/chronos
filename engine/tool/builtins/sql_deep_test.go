package builtins

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQL_SelectSyntaxError_Deep(t *testing.T) {
	db := setupTestDB(t)
	tool := NewSQLTool(db, nil)
	_, err := tool.Handler(context.Background(), map[string]any{
		"query": "SELECT * FROM not_a_real_table_ever",
	})
	if err == nil {
		t.Fatal("expected query error")
	}
}

func TestSQL_ExecSyntaxError_Deep(t *testing.T) {
	db := setupTestDB(t)
	tool := NewSQLTool(db, []string{"SELECT", "INSERT"})
	_, err := tool.Handler(context.Background(), map[string]any{
		"query": "INSERT INTO users (nope) VALUES (1)",
	})
	if err == nil {
		t.Fatal("expected exec error")
	}
}

func TestSQL_WithClause_Deep(t *testing.T) {
	db := setupTestDB(t)
	tool := NewSQLTool(db, []string{"SELECT", "WITH"})
	res, err := tool.Handler(context.Background(), map[string]any{
		"query": "WITH x AS (SELECT 1 AS n) SELECT n FROM x",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]any)
	if m["count"].(int) != 1 {
		t.Fatalf("count=%v", m["count"])
	}
}

func TestSQL_OperationWhitespacePrefix_Deep(t *testing.T) {
	db := setupTestDB(t)
	tool := NewSQLTool(db, nil)
	_, err := tool.Handler(context.Background(), map[string]any{
		"query": "  \n\tSELECT 1",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSQL_DisallowedUpdate_Deep(t *testing.T) {
	db := setupTestDB(t)
	tool := NewSQLTool(db, []string{"SELECT"})
	_, err := tool.Handler(context.Background(), map[string]any{
		"query": "UPDATE users SET name = 'x' WHERE id = 1",
	})
	if err == nil {
		t.Fatal("expected operation not allowed")
	}
}
