package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

// newIntegrationStore connects to a real PostgreSQL instance identified by the
// CHRONOS_PG_DSN environment variable. Tests using it are skipped when the
// variable is unset, so the default `go test` run needs no database. To run
// them locally, e.g.:
//
//	docker run -e POSTGRES_PASSWORD=pg -p 5432:5432 -d postgres:16
//	CHRONOS_PG_DSN='postgres://postgres:pg@localhost:5432/postgres?sslmode=disable' \
//	  go test ./storage/adapters/postgres/ -run Integration
func newIntegrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CHRONOS_PG_DSN")
	if dsn == "" {
		t.Skip("CHRONOS_PG_DSN not set; skipping Postgres integration test")
	}
	store, err := New(dsn, WithMaxOpenConns(4))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestIntegration_PaginationAndRetention(t *testing.T) {
	store := newIntegrationStore(t)
	ctx := context.Background()
	sid := fmt.Sprintf("sess-%d", time.Now().UnixNano())

	for i := 1; i <= 5; i++ {
		if err := store.AppendEvent(ctx, &storage.Event{
			ID: fmt.Sprintf("%s-e%d", sid, i), SessionID: sid, SeqNum: int64(i),
			Type: "x", Payload: map[string]any{"i": i}, CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	var total, pages int
	cursor := ""
	for {
		page, err := store.ListEventsPaged(ctx, sid, 0, 2, cursor)
		if err != nil {
			t.Fatalf("ListEventsPaged: %v", err)
		}
		pages++
		total += len(page.Events)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if total != 5 || pages != 3 {
		t.Errorf("pagination total=%d pages=%d, want 5/3", total, pages)
	}

	n, err := store.TrimEvents(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("TrimEvents: %v", err)
	}
	if n != 5 {
		t.Errorf("trimmed = %d, want 5", n)
	}
}

func TestIntegration_BatchCopyTraces(t *testing.T) {
	store := newIntegrationStore(t)
	ctx := context.Background()
	sid := fmt.Sprintf("sess-%d", time.Now().UnixNano())

	traces := make([]*storage.Trace, 100)
	base := time.Now()
	for i := range traces {
		traces[i] = &storage.Trace{
			ID: fmt.Sprintf("%s-t%d", sid, i), SessionID: sid, Name: "span", Kind: "node",
			Input: map[string]any{"i": i}, StartedAt: base.Add(time.Duration(i) * time.Millisecond),
		}
	}
	if err := store.InsertTraces(ctx, traces); err != nil {
		t.Fatalf("InsertTraces (COPY): %v", err)
	}
	got, err := store.ListTraces(ctx, sid)
	if err != nil {
		t.Fatalf("ListTraces: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("got %d traces, want 100", len(got))
	}

	// Batch events with idempotency.
	events := []*storage.Event{
		{ID: sid + "-b1", SessionID: sid, SeqNum: 1, Type: "x", CreatedAt: time.Now()},
		{ID: sid + "-b2", SessionID: sid, SeqNum: 2, Type: "y", CreatedAt: time.Now()},
	}
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents idempotent: %v", err)
	}
	evs, _ := store.ListEvents(ctx, sid, 0)
	if len(evs) != 2 {
		t.Errorf("events = %d, want 2", len(evs))
	}
}
