package builtins

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestSQLTool_WITHQuery_Boost(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.ExecContext(context.Background(), `CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT);`)
	_, _ = db.ExecContext(context.Background(), `INSERT INTO t (v) VALUES ('a');`)

	def := NewSQLTool(db, []string{"SELECT", "WITH"})
	h := def.Handler
	out, err := h(context.Background(), map[string]any{
		"query": "WITH c AS (SELECT 1 AS n) SELECT n FROM c",
	})
	if err != nil {
		t.Fatalf("WITH query: %v", err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["count"].(int) != 1 {
		t.Fatalf("unexpected result %#v", out)
	}
}

func TestSQLTool_ExecInsert_Boost(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, _ = db.ExecContext(context.Background(), `CREATE TABLE u (id INTEGER PRIMARY KEY AUTOINCREMENT, v TEXT);`)

	def := NewSQLTool(db, []string{"INSERT", "SELECT"})
	h := def.Handler
	out, err := h(context.Background(), map[string]any{
		"query": "INSERT INTO u (v) VALUES (?)",
		"params": []any{"hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	if m["rows_affected"].(int64) != 1 {
		t.Errorf("rows_affected = %v", m["rows_affected"])
	}
}

func TestSQLTool_OperationDenied_Boost(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	def := NewSQLTool(db, []string{"SELECT"})
	h := def.Handler
	_, err := h(context.Background(), map[string]any{"query": "DELETE FROM x"})
	if err == nil {
		t.Fatal("expected op denied")
	}
}

func TestSQLTool_DefaultAllowsSelectOnly_Boost(t *testing.T) {
	db, _ := sql.Open("sqlite3", ":memory:")
	defer db.Close()
	def := NewSQLTool(db, nil)
	if def.Handler == nil {
		t.Fatal("nil handler")
	}
	_, err := def.Handler(context.Background(), map[string]any{"query": "INSERT INTO x VALUES (1)"})
	if err == nil {
		t.Fatal("expected insert denied with default ops")
	}
}

func TestEvaluate_UnaryAndPower_Boost(t *testing.T) {
	tests := []struct {
		expr string
		want float64
	}{
		{"(-3)^2", 9},
		{"2^-2", 0.25},
		{"-(1+2)", -3},
	}
	for _, tt := range tests {
		got, err := evaluate(tt.expr)
		if err != nil {
			t.Errorf("%s: %v", tt.expr, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s = %v, want %v", tt.expr, got, tt.want)
		}
	}
}

func TestFileWriteTool_MkdirAllFails_Boost(t *testing.T) {
	root := t.TempDir()
	block := filepath.Join(root, "notadir")
	if err := os.WriteFile(block, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	def := NewFileWriteTool(root)
	_, err := def.Handler(context.Background(), map[string]any{
		"path":    "notadir/sub/file.txt",
		"content": "nope",
	})
	if err == nil {
		t.Fatal("expected mkdir error when path component is a file")
	}
}
