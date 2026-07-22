package scheduler

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	_ "modernc.org/sqlite"
)

// sharedDSN returns a WAL-mode SQLite DSN with immediate transaction locking so
// multiple independent *sql.DB handles (simulating distributed scheduler
// replicas) contend on the same file with serialized writers.
func sharedDSN(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scheduler.db")
	return path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate"
}

func openSchedStore(t *testing.T, dsn string) *SQLStore {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return NewSQLStore(db, DialectSQLite)
}

func newSchedStore(t *testing.T) *SQLStore {
	t.Helper()
	s := openSchedStore(t, sharedDSN(t))
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

func TestStoreScheduler_AddGetListRemove(t *testing.T) {
	store := newSchedStore(t)
	s := NewStoreScheduler(store, nil)

	sched, err := s.Add("agent-1", "*/5 * * * *", "hello", true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if sched.AgentID != "agent-1" || !sched.Enabled {
		t.Fatalf("unexpected schedule: %+v", sched)
	}
	if sched.NextRunAt.IsZero() {
		t.Fatal("NextRunAt should be set")
	}

	got, err := s.Get(sched.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != sched.ID || got.CronExpr != "*/5 * * * *" {
		t.Fatalf("Get mismatch: %+v", got)
	}

	if n := len(s.List()); n != 1 {
		t.Fatalf("List len = %d, want 1", n)
	}

	if err := s.Remove(sched.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if n := len(s.List()); n != 0 {
		t.Fatalf("List len = %d after remove, want 0", n)
	}
}

func TestStoreScheduler_InvalidCron(t *testing.T) {
	s := NewStoreScheduler(newSchedStore(t), nil)
	if _, err := s.Add("a", "not a cron", "", true); err == nil {
		t.Fatal("expected error for invalid cron")
	}
}

func TestStoreScheduler_FiresAndRecordsHistory(t *testing.T) {
	store := newSchedStore(t)
	var fired int32
	s := NewStoreScheduler(store, func(_ context.Context, _, _, _ string) error {
		atomic.AddInt32(&fired, 1)
		return nil
	})

	sched, err := s.Add("agent", "* * * * *", "in", true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Drive one deterministic cycle at the schedule's due instant.
	s.Tick(context.Background(), sched.NextRunAt)

	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Fatalf("fired = %d, want 1", got)
	}
	hist := s.History(sched.ID)
	if len(hist) != 1 {
		t.Fatalf("history len = %d, want 1", len(hist))
	}
	if hist[0].Status != "success" {
		t.Fatalf("status = %q, want success", hist[0].Status)
	}

	// A second tick at the same instant must NOT re-fire: NextRunAt was advanced.
	s.Tick(context.Background(), sched.NextRunAt)
	if got := atomic.LoadInt32(&fired); got != 1 {
		t.Fatalf("re-fired at same instant: fired = %d, want 1", got)
	}
}

// TestStoreScheduler_SingleFireAcrossReplicas is the core exactly-once proof: two
// StoreScheduler instances over independent DB handles share one store, and a
// single due schedule is claimed and fired by exactly one of them. Run with -race.
func TestStoreScheduler_SingleFireAcrossReplicas(t *testing.T) {
	dsn := sharedDSN(t)
	storeA := openSchedStore(t, dsn)
	if err := storeA.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	storeB := openSchedStore(t, dsn)

	var total int32
	fire := func(_ context.Context, _, _, _ string) error {
		atomic.AddInt32(&total, 1)
		return nil
	}
	schedA := NewStoreScheduler(storeA, fire)
	schedB := NewStoreScheduler(storeB, fire)

	created, err := schedA.Add("agent", "* * * * *", "payload", true)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	due := created.NextRunAt

	// Both replicas tick at the same due instant concurrently.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); schedA.Tick(context.Background(), due) }()
	go func() { defer wg.Done(); schedB.Tick(context.Background(), due) }()
	wg.Wait()

	if got := atomic.LoadInt32(&total); got != 1 {
		t.Fatalf("total fires across replicas = %d, want exactly 1", got)
	}

	// Exactly one history record exists for the schedule.
	hist, err := storeA.History(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("history records = %d, want 1", len(hist))
	}
}

func TestStoreScheduler_SessionReuse(t *testing.T) {
	store := newSchedStore(t)
	var sessions []string
	var mu sync.Mutex
	s := NewStoreScheduler(store, func(_ context.Context, _, _, sessionID string) error {
		mu.Lock()
		sessions = append(sessions, sessionID)
		mu.Unlock()
		return nil
	})

	sched, err := s.Add("agent", "* * * * *", "in", false) // reuse session
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	s.Tick(context.Background(), sched.NextRunAt)
	reloaded, err := store.Get(context.Background(), sched.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Next fire should reuse the persisted session.
	s.Tick(context.Background(), reloaded.NextRunAt)

	mu.Lock()
	defer mu.Unlock()
	if len(sessions) != 2 {
		t.Fatalf("fired %d times, want 2", len(sessions))
	}
	if sessions[0] != sessions[1] {
		t.Fatalf("session not reused: %q vs %q", sessions[0], sessions[1])
	}
}

// Runner interface conformance.
var (
	_ Runner = (*Scheduler)(nil)
	_ Runner = (*StoreScheduler)(nil)
)
