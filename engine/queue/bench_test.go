package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkEnqueue measures the durable enqueue hot path (admission + insert).
func BenchmarkEnqueue(b *testing.B) {
	q := newBenchQueue(b, Config{})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Enqueue(ctx, &Run{
			SessionID: fmt.Sprintf("sess-%d", i),
			GraphID:   "g",
			Payload:   []byte(`{"n":1}`),
		}); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
	}
}

// BenchmarkDequeue measures the leased-dequeue hot path (the atomic claim). The
// queue is pre-filled so every iteration claims a real run.
func BenchmarkDequeue(b *testing.B) {
	q := newBenchQueue(b, Config{})
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		if err := q.Enqueue(ctx, &Run{SessionID: fmt.Sprintf("s-%d", i), GraphID: "g"}); err != nil {
			b.Fatalf("prefill enqueue: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Dequeue(ctx, "worker", 30*time.Second); err != nil {
			b.Fatalf("dequeue: %v", err)
		}
	}
}

// BenchmarkEnqueueDequeueComplete measures a full lifecycle round-trip:
// enqueue -> leased dequeue -> complete. This is the per-run steady-state cost.
func BenchmarkEnqueueDequeueComplete(b *testing.B) {
	q := newBenchQueue(b, Config{})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Enqueue(ctx, &Run{SessionID: fmt.Sprintf("s-%d", i), GraphID: "g"}); err != nil {
			b.Fatalf("enqueue: %v", err)
		}
		r, err := q.Dequeue(ctx, "worker", 30*time.Second)
		if err != nil {
			b.Fatalf("dequeue: %v", err)
		}
		if err := q.Complete(ctx, r.ID, "worker", StatusCompleted, ""); err != nil {
			b.Fatalf("complete: %v", err)
		}
	}
}

// BenchmarkHeartbeat measures the lease-extension hot path called on every
// worker heartbeat tick.
func BenchmarkHeartbeat(b *testing.B) {
	q := newBenchQueue(b, Config{})
	ctx := context.Background()

	if err := q.Enqueue(ctx, &Run{SessionID: "s", GraphID: "g"}); err != nil {
		b.Fatalf("enqueue: %v", err)
	}
	r, err := q.Dequeue(ctx, "worker", time.Hour)
	if err != nil {
		b.Fatalf("dequeue: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := q.Heartbeat(ctx, r.ID, "worker", time.Hour); err != nil {
			b.Fatalf("heartbeat: %v", err)
		}
	}
}

// BenchmarkDequeueEmpty measures the cost of an empty poll — the common case
// for an idle worker pool.
func BenchmarkDequeueEmpty(b *testing.B) {
	q := newBenchQueue(b, Config{})
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := q.Dequeue(ctx, "worker", 30*time.Second); err != nil && !errors.Is(err, ErrEmpty) {
			b.Fatalf("dequeue: %v", err)
		}
	}
}

// newBenchStore opens a migrated SQLite-backed store for benchmarks. It mirrors
// the *testing.T helper in helpers_test.go but takes a *testing.B.
func newBenchStore(b *testing.B) *SQLStore {
	b.Helper()
	path := filepath.Join(b.TempDir(), "queue.db")
	dsn := path + "?_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(4)
	b.Cleanup(func() { _ = db.Close() })
	s := NewSQLStore(db, DialectSQLite)
	if err := s.Migrate(context.Background()); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	return s
}

// newBenchQueue builds a migrated SQLite-backed queue for benchmarks.
func newBenchQueue(b *testing.B, cfg Config) *Queue {
	b.Helper()
	return New(newBenchStore(b), cfg)
}
