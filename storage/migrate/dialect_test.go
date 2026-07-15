package migrate

import (
	"context"
	"testing"
)

func TestRebind(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		in      string
		want    string
	}{
		{"sqlite unchanged", DialectSQLite, "INSERT INTO t VALUES (?, ?, ?)", "INSERT INTO t VALUES (?, ?, ?)"},
		{"postgres numbered", DialectPostgres, "INSERT INTO t VALUES (?, ?, ?)", "INSERT INTO t VALUES ($1, $2, $3)"},
		{"postgres single", DialectPostgres, "DELETE FROM t WHERE v = ?", "DELETE FROM t WHERE v = $1"},
		{"postgres none", DialectPostgres, "SELECT 1", "SELECT 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(nil, WithDialect(tt.dialect))
			if got := m.rebind(tt.in); got != tt.want {
				t.Errorf("rebind() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewDefaultDialectIsSQLite(t *testing.T) {
	m := New(nil)
	if m.dialect != DialectSQLite {
		t.Errorf("default dialect = %d, want DialectSQLite", m.dialect)
	}
}

// TestMigrate_SQLiteViaFramework verifies the migrator applies a multi-statement
// schema through a single pinned connection (as adapters now do).
func TestMigrate_SQLiteViaFramework(t *testing.T) {
	db := testDB(t)
	m := New(db, WithDialect(DialectSQLite))
	m.Add(1, "schema", `
CREATE TABLE a (id INTEGER PRIMARY KEY);
CREATE INDEX idx_a ON a(id);
CREATE TABLE b (id INTEGER PRIMARY KEY);
`, "")

	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// Idempotent second run.
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if _, err := db.Exec("INSERT INTO a (id) VALUES (1)"); err != nil {
		t.Fatalf("table a missing: %v", err)
	}
	if _, err := db.Exec("INSERT INTO b (id) VALUES (1)"); err != nil {
		t.Fatalf("table b missing: %v", err)
	}
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.CurrentVersion != 1 {
		t.Errorf("version = %d, want 1", status.CurrentVersion)
	}
}
