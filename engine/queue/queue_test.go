package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestQueue_AdmissionControl(t *testing.T) {
	tests := []struct {
		name       string
		policy     AdmissionPolicy
		maxDepth   int
		enqueue    int
		wantErr    error
		wantParked int
	}{
		{"unbounded admits all", "", 0, 5, nil, 0},
		{"reject past capacity", PolicyReject, 2, 3, ErrOverloaded, 0},
		{"park past capacity", PolicyPark, 2, 3, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			q := New(s, Config{MaxDepth: tc.maxDepth, Policy: tc.policy})
			ctx := context.Background()

			var lastErr error
			for i := 0; i < tc.enqueue; i++ {
				lastErr = q.Enqueue(ctx, &Run{SessionID: "s"})
			}
			if !errors.Is(lastErr, tc.wantErr) {
				t.Fatalf("last enqueue err = %v, want %v", lastErr, tc.wantErr)
			}
			parked, err := s.CountByStatus(ctx, StatusParked)
			if err != nil {
				t.Fatalf("count parked: %v", err)
			}
			if parked != tc.wantParked {
				t.Fatalf("parked = %d, want %d", parked, tc.wantParked)
			}
		})
	}
}

func TestQueue_ReleaseAdmissionParked(t *testing.T) {
	s := newStore(t)
	q := New(s, Config{MaxDepth: 2, Policy: PolicyPark})
	ctx := context.Background()

	// Fill capacity (2 pending) then park a third.
	for i := 0; i < 3; i++ {
		if err := q.Enqueue(ctx, &Run{SessionID: "s"}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}
	// No slack yet: release should promote nothing.
	n, err := q.ReleaseAdmissionParked(ctx, 10)
	if err != nil {
		t.Fatalf("release (no slack): %v", err)
	}
	if n != 0 {
		t.Fatalf("released %d with no slack, want 0", n)
	}

	// Drain one pending run to create slack, then release.
	if _, err = q.Dequeue(ctx, "w1", time.Minute); err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err = q.Complete(ctx, mustLeasedID(t, s), "w1", StatusCompleted, ""); err != nil {
		// mustLeasedID returns the currently-leased run id.
		t.Fatalf("complete: %v", err)
	}
	n, err = q.ReleaseAdmissionParked(ctx, 10)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if n != 1 {
		t.Fatalf("released %d, want 1", n)
	}
}

// mustLeasedID returns the id of the single currently-leased run.
func mustLeasedID(t *testing.T, s *SQLStore) string {
	t.Helper()
	row := s.db.QueryRowContext(context.Background(),
		`SELECT id FROM queue_runs WHERE status='`+StatusLeased+`' LIMIT 1`)
	var id string
	if err := row.Scan(&id); err != nil {
		t.Fatalf("find leased run: %v", err)
	}
	return id
}

func TestQueue_SignalRequiresFields(t *testing.T) {
	s := newStore(t)
	q := New(s, Config{})
	if _, err := q.Signal(context.Background(), &Signal{Name: "x"}); err == nil {
		t.Fatal("want error for missing session id")
	}
}

func TestOutbox_RecordDrainIdempotent(t *testing.T) {
	s := newStore(t)
	ob := NewOutbox(s)
	ctx := context.Background()

	// Record the same effect twice (as a retry would) — one row.
	for i := 0; i < 2; i++ {
		if err := ob.Record(ctx, "sess", "eff-1", "webhook", []byte(`{"k":1}`)); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	var dispatched int
	d := DispatcherFunc(func(ctx context.Context, e *OutboxEntry) error {
		dispatched++
		return nil
	})
	sent, err := ob.DrainOnce(ctx, d, 10)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if sent != 1 || dispatched != 1 {
		t.Fatalf("sent=%d dispatched=%d, want 1/1", sent, dispatched)
	}
	// Second drain has nothing left.
	sent, err = ob.DrainOnce(ctx, d, 10)
	if err != nil {
		t.Fatalf("drain 2: %v", err)
	}
	if sent != 0 {
		t.Fatalf("second drain sent=%d, want 0", sent)
	}
}

func TestOutbox_FailedStaysPending(t *testing.T) {
	s := newStore(t)
	ob := NewOutbox(s)
	ctx := context.Background()
	if err := ob.Record(ctx, "sess", "eff-2", "webhook", nil); err != nil {
		t.Fatalf("record: %v", err)
	}
	failing := DispatcherFunc(func(ctx context.Context, e *OutboxEntry) error {
		return errors.New("boom")
	})
	sent, err := ob.DrainOnce(ctx, failing, 10)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if sent != 0 {
		t.Fatalf("sent=%d, want 0", sent)
	}
	// Entry remains claimable for retry.
	entries, err := s.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(entries) != 1 || entries[0].Attempts != 1 {
		t.Fatalf("want 1 pending w/ 1 attempt, got %+v", entries)
	}
}
