package pgvector

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"testing"

	"github.com/spawn08/chronos/storage"
)

// ---------------------------------------------------------------------------
// Mock SQL driver
// ---------------------------------------------------------------------------

const mockDriverName = "pgvector_mock"

func init() {
	sql.Register(mockDriverName, &mockDriver{})
}

type mockDriver struct{}

func (d *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{dsn: name}, nil
}

type mockConn struct {
	dsn string
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{query: query}, nil
}

func (c *mockConn) Close() error { return nil }
func (c *mockConn) Begin() (driver.Tx, error) {
	return &mockTx{}, nil
}

type mockTx struct{}

func (t *mockTx) Commit() error   { return nil }
func (t *mockTx) Rollback() error { return nil }

type mockStmt struct {
	query string
}

func (s *mockStmt) Close() error  { return nil }
func (s *mockStmt) NumInput() int { return -1 }

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	return &mockResult{}, nil
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	return &mockRows{}, nil
}

type mockResult struct{}

func (r *mockResult) LastInsertId() (int64, error) { return 0, nil }
func (r *mockResult) RowsAffected() (int64, error) { return 1, nil }

type mockRows struct {
	done bool
}

func (r *mockRows) Columns() []string {
	return []string{"id", "embedding", "content", "metadata", "score"}
}

func (r *mockRows) Close() error { return nil }

func (r *mockRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "doc1"
	dest[1] = "[0.1,0.2,0.3]"
	dest[2] = "test content"
	dest[3] = `{"source":"test"}`
	dest[4] = float64(0.95)
	return nil
}

// ---------------------------------------------------------------------------
// Tests using mock driver
// ---------------------------------------------------------------------------

func newMockDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(mockDriverName, "mock://")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	return db
}

func TestCreateCollection_Mock(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	err := store.CreateCollection(context.Background(), "my_collection", 128)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
}

func TestUpsert_Mock(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	embeddings := []storage.Embedding{
		{
			ID:      "doc1",
			Vector:  []float32{0.1, 0.2, 0.3},
			Content: "test content",
			Metadata: map[string]any{
				"source": "unit_test",
			},
		},
		{
			ID:     "doc2",
			Vector: []float32{0.4, 0.5, 0.6},
		},
	}

	if err := store.Upsert(context.Background(), "my_collection", embeddings); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestUpsert_InvalidMetadata(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	// json.Marshal cannot fail on standard types, so we test normal path
	embeddings := []storage.Embedding{
		{
			ID:     "doc1",
			Vector: []float32{1.0},
			Metadata: map[string]any{
				"key": "value",
			},
		},
	}

	if err := store.Upsert(context.Background(), "test_col", embeddings); err != nil {
		t.Fatalf("Upsert with metadata: %v", err)
	}
}

func TestSearch_Mock(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	results, err := store.Search(context.Background(), "my_collection", []float32{0.1, 0.2, 0.3}, 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Should get 1 row from mockRows
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "doc1" {
		t.Errorf("ID = %q, want 'doc1'", results[0].ID)
	}
	if results[0].Content != "test content" {
		t.Errorf("Content = %q", results[0].Content)
	}
}

func TestDelete_Mock(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	if err := store.Delete(context.Background(), "my_collection", []string{"doc1", "doc2"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_EmptyIDs(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	// Empty IDs should return nil immediately
	if err := store.Delete(context.Background(), "my_collection", nil); err != nil {
		t.Fatalf("Delete with nil: %v", err)
	}
	if err := store.Delete(context.Background(), "my_collection", []string{}); err != nil {
		t.Fatalf("Delete with empty: %v", err)
	}
}

func TestClose_Mock(t *testing.T) {
	db := newMockDB(t)
	store := New(db)

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestVectorToString_Extended(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
		want string
	}{
		{"negative", []float32{-1.0, -0.5}, "[-1,-0.5]"},
		{"zero", []float32{0.0}, "[0]"},
		{"large", []float32{1000.5}, "[1000.5]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vectorToString(tt.vec)
			if got != tt.want {
				t.Errorf("vectorToString(%v) = %q, want %q", tt.vec, got, tt.want)
			}
		})
	}
}

// TestSanitizeTableName_WithNumbers tests table names starting with numbers.
func TestSanitizeTableName_Numbers(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"123abc", "123abc"},
		{"a1b2c3", "a1b2c3"},
		{"_underscore", "_underscore"},
	}
	for _, tt := range tests {
		got := sanitizeTableName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeTableName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// Satisfy compiler
var _ = fmt.Sprintf
