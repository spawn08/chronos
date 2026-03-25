package migrate

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMigrator_Rollback_NoApplied_Squeeze(t *testing.T) {
	db := testDB(t)
	m := New(db).Add(1, "noop", "CREATE TABLE IF NOT EXISTS t_squeeze (id INT)", "DROP TABLE t_squeeze")
	err := m.Rollback(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no migrations") {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestMigrator_Rollback_NoDownSQL_Squeeze(t *testing.T) {
	db := testDB(t)
	m := New(db).Add(1, "up only", "CREATE TABLE t_nd (id INT)", "")
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := m.Rollback(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no rollback SQL") {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestMigrator_Rollback_VersionNotInRegistry_Squeeze(t *testing.T) {
	db := testDB(t)
	m := New(db).Add(1, "first", "CREATE TABLE t_reg (id INT)", "DROP TABLE t_reg")
	if err := m.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO _migrations (version, description, applied_at) VALUES (?, ?, ?)`,
		2, "ghost", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	err = m.Rollback(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not found in registry") {
		t.Fatalf("Rollback: %v", err)
	}
}

func TestMigrator_Status_AllPending_Squeeze(t *testing.T) {
	db := testDB(t)
	m := New(db).Add(1, "a", "CREATE TABLE st_pending (id INT)", "DROP TABLE st_pending")
	st, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentVersion != 0 || len(st.Pending) != 1 || len(st.Applied) != 0 {
		t.Fatalf("status: %+v", st)
	}
}
