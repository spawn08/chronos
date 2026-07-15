package sqlite

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
)

func seedEvents(t *testing.T, store *Store, sessionID string, n int) {
	t.Helper()
	ctx := context.Background()
	for i := 1; i <= n; i++ {
		e := &storage.Event{
			ID:        fmt.Sprintf("%s-e%d", sessionID, i),
			SessionID: sessionID,
			SeqNum:    int64(i),
			Type:      "node_enter",
			Payload:   map[string]any{"i": i},
			CreatedAt: time.Now(),
		}
		if err := store.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}
}

func TestListEventsPaged(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedEvents(t, store, "s1", 5)

	tests := []struct {
		name      string
		limit     int
		wantPages int
		wantTotal int
	}{
		{"page size 2", 2, 3, 5},
		{"page size 5 single page", 5, 1, 5},
		{"page size 10 single page", 10, 1, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var total, pages int
			cursor := ""
			for {
				page, err := store.ListEventsPaged(ctx, "s1", 0, tt.limit, cursor)
				if err != nil {
					t.Fatalf("ListEventsPaged: %v", err)
				}
				pages++
				total += len(page.Events)
				// Verify ordering ascending.
				for i := 1; i < len(page.Events); i++ {
					if page.Events[i].SeqNum <= page.Events[i-1].SeqNum {
						t.Fatalf("events not ascending")
					}
				}
				if page.NextCursor == "" {
					break
				}
				cursor = page.NextCursor
				if pages > 10 {
					t.Fatal("pagination did not terminate")
				}
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if pages != tt.wantPages {
				t.Errorf("pages = %d, want %d", pages, tt.wantPages)
			}
		})
	}
}

func TestListEventsPaged_AfterSeq(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	seedEvents(t, store, "s1", 5)

	page, err := store.ListEventsPaged(ctx, "s1", 3, 100, "")
	if err != nil {
		t.Fatalf("ListEventsPaged: %v", err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("want 2 events after seq 3, got %d", len(page.Events))
	}
	if page.Events[0].SeqNum != 4 {
		t.Errorf("first seq = %d, want 4", page.Events[0].SeqNum)
	}
}

func TestListCheckpointsPaged(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		cp := &storage.Checkpoint{
			ID: fmt.Sprintf("cp%d", i), SessionID: "s1", RunID: "r1",
			NodeID: fmt.Sprintf("n%d", i), SeqNum: int64(i),
			State: map[string]any{"i": i}, CreatedAt: time.Now(),
		}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	}

	var total, pages int
	cursor := ""
	for {
		page, err := store.ListCheckpointsPaged(ctx, "s1", 2, cursor)
		if err != nil {
			t.Fatalf("ListCheckpointsPaged: %v", err)
		}
		pages++
		total += len(page.Checkpoints)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if pages != 3 {
		t.Errorf("pages = %d, want 3", pages)
	}
}

func TestListTracesPaged(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	base := time.Now().Truncate(time.Second)
	for i := 1; i <= 5; i++ {
		tr := &storage.Trace{
			ID: fmt.Sprintf("t%d", i), SessionID: "s1", Name: "span", Kind: "node",
			StartedAt: base.Add(time.Duration(i) * time.Millisecond),
		}
		if err := store.InsertTrace(ctx, tr); err != nil {
			t.Fatalf("InsertTrace: %v", err)
		}
	}

	var got []string
	cursor := ""
	pages := 0
	for {
		page, err := store.ListTracesPaged(ctx, "s1", 2, cursor)
		if err != nil {
			t.Fatalf("ListTracesPaged: %v", err)
		}
		pages++
		for _, tr := range page.Traces {
			got = append(got, tr.ID)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("did not terminate")
		}
	}
	if len(got) != 5 {
		t.Fatalf("got %d traces, want 5", len(got))
	}
	want := []string{"t1", "t2", "t3", "t4", "t5"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch: got %v, want %v", got, want)
		}
	}
}

func TestRetentionTrim(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()

	// Events: one old, one recent.
	_ = store.AppendEvent(ctx, &storage.Event{ID: "eo", SessionID: "s1", SeqNum: 1, Type: "x", CreatedAt: old})
	_ = store.AppendEvent(ctx, &storage.Event{ID: "en", SessionID: "s1", SeqNum: 2, Type: "x", CreatedAt: recent})
	// Traces.
	_ = store.InsertTrace(ctx, &storage.Trace{ID: "to", SessionID: "s1", Name: "n", Kind: "node", StartedAt: old})
	_ = store.InsertTrace(ctx, &storage.Trace{ID: "tn", SessionID: "s1", Name: "n", Kind: "node", StartedAt: recent})
	// Audit logs.
	_ = store.AppendAuditLog(ctx, &storage.AuditLog{ID: "ao", SessionID: "s1", Actor: "u", Action: "a", Resource: "r", CreatedAt: old})
	_ = store.AppendAuditLog(ctx, &storage.AuditLog{ID: "an", SessionID: "s1", Actor: "u", Action: "a", Resource: "r", CreatedAt: recent})

	cutoff := time.Now().Add(-24 * time.Hour)

	n, err := store.TrimEvents(ctx, cutoff)
	if err != nil || n != 1 {
		t.Fatalf("TrimEvents = %d, %v; want 1", n, err)
	}
	if n, err = store.TrimTraces(ctx, cutoff); err != nil || n != 1 {
		t.Fatalf("TrimTraces = %d, %v; want 1", n, err)
	}
	if n, err = store.TrimAuditLogs(ctx, cutoff); err != nil || n != 1 {
		t.Fatalf("TrimAuditLogs = %d, %v; want 1", n, err)
	}

	events, _ := store.ListEvents(ctx, "s1", 0)
	if len(events) != 1 || events[0].ID != "en" {
		t.Errorf("after trim events = %+v, want only en", events)
	}
}

func TestTrimCheckpoints(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		cp := &storage.Checkpoint{
			ID: fmt.Sprintf("cp%d", i), SessionID: "s1", RunID: "r1",
			NodeID: "n", SeqNum: int64(i), State: map[string]any{}, CreatedAt: time.Now(),
		}
		if err := store.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatalf("SaveCheckpoint: %v", err)
		}
	}

	tests := []struct {
		name       string
		keep       int
		wantRemove int64
		wantLeft   int
	}{
		{"keep 2", 2, 3, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := store.TrimCheckpoints(ctx, "s1", tt.keep)
			if err != nil {
				t.Fatalf("TrimCheckpoints: %v", err)
			}
			if n != tt.wantRemove {
				t.Errorf("removed = %d, want %d", n, tt.wantRemove)
			}
			left, _ := store.ListCheckpoints(ctx, "s1")
			if len(left) != tt.wantLeft {
				t.Errorf("left = %d, want %d", len(left), tt.wantLeft)
			}
			// The newest (highest seq) must survive.
			latest, _ := store.GetLatestCheckpoint(ctx, "s1")
			if latest.SeqNum != 5 {
				t.Errorf("latest seq = %d, want 5", latest.SeqNum)
			}
		})
	}

	if n, _ := store.TrimCheckpoints(ctx, "s1", 0); n != 0 {
		t.Errorf("keep=0 should be no-op, removed %d", n)
	}
}

func TestApplyRetention(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	old := time.Now().Add(-72 * time.Hour)
	_ = store.AppendEvent(ctx, &storage.Event{ID: "eo", SessionID: "s1", SeqNum: 1, Type: "x", CreatedAt: old})
	_ = store.InsertTrace(ctx, &storage.Trace{ID: "to", SessionID: "s1", Name: "n", Kind: "node", StartedAt: old})
	for i := 1; i <= 4; i++ {
		_ = store.SaveCheckpoint(ctx, &storage.Checkpoint{
			ID: fmt.Sprintf("cp%d", i), SessionID: "s1", RunID: "r", NodeID: "n",
			SeqNum: int64(i), State: map[string]any{}, CreatedAt: time.Now(),
		})
	}

	res, err := storage.ApplyRetention(ctx, store, storage.RetentionPolicy{
		MaxAge:                    24 * time.Hour,
		KeepCheckpointsPerSession: 2,
	}, []string{"s1"})
	if err != nil {
		t.Fatalf("ApplyRetention: %v", err)
	}
	if res.Events != 1 {
		t.Errorf("events trimmed = %d, want 1", res.Events)
	}
	if res.Traces != 1 {
		t.Errorf("traces trimmed = %d, want 1", res.Traces)
	}
	if res.Checkpoints != 2 {
		t.Errorf("checkpoints trimmed = %d, want 2", res.Checkpoints)
	}
}

func TestAppendEventsBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	events := []*storage.Event{
		{ID: "b1", SessionID: "s1", SeqNum: 1, Type: "x", Payload: map[string]any{"a": 1}, CreatedAt: time.Now()},
		{ID: "b2", SessionID: "s1", SeqNum: 2, Type: "y", Payload: map[string]any{"b": 2}, CreatedAt: time.Now()},
		{ID: "b3", SessionID: "s1", SeqNum: 3, Type: "z", Payload: map[string]any{"c": 3}, CreatedAt: time.Now()},
	}
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents: %v", err)
	}
	got, _ := store.ListEvents(ctx, "s1", 0)
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}

	// Idempotent: re-appending the same batch is a no-op.
	if err := store.AppendEvents(ctx, events); err != nil {
		t.Fatalf("AppendEvents idempotent: %v", err)
	}
	got, _ = store.ListEvents(ctx, "s1", 0)
	if len(got) != 3 {
		t.Fatalf("after re-append got %d events, want 3", len(got))
	}

	// Empty batch is a no-op.
	if err := store.AppendEvents(ctx, nil); err != nil {
		t.Fatalf("AppendEvents(nil): %v", err)
	}
}

func TestInsertTracesBatch(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	traces := []*storage.Trace{
		{ID: "bt1", SessionID: "s1", Name: "a", Kind: "node", StartedAt: time.Now()},
		{ID: "bt2", SessionID: "s1", Name: "b", Kind: "node", StartedAt: time.Now()},
	}
	if err := store.InsertTraces(ctx, traces); err != nil {
		t.Fatalf("InsertTraces: %v", err)
	}
	got, _ := store.ListTraces(ctx, "s1")
	if len(got) != 2 {
		t.Fatalf("got %d traces, want 2", len(got))
	}
	if err := store.InsertTraces(ctx, nil); err != nil {
		t.Fatalf("InsertTraces(nil): %v", err)
	}
}

func TestNewWithPoolOptions(t *testing.T) {
	store, err := New(":memory:",
		WithMaxOpenConns(3),
		WithMaxIdleConns(2),
		WithConnMaxLifetime(time.Minute),
	)
	if err != nil {
		t.Fatalf("New with options: %v", err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	stats := store.db.Stats()
	if stats.MaxOpenConnections != 3 {
		t.Errorf("MaxOpenConnections = %d, want 3", stats.MaxOpenConnections)
	}
}
