package queue

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func mustEnqueue(t *testing.T, s *SQLStore, r *Run) *Run {
	t.Helper()
	now := time.Now()
	if r.ID == "" {
		r.ID = "r_" + time.Now().Format("150405.000000000")
	}
	if r.Kind == "" {
		r.Kind = KindStart
	}
	if r.Status == "" {
		r.Status = StatusPending
	}
	if r.MaxAttempts == 0 {
		r.MaxAttempts = 3
	}
	if r.AvailableAt.IsZero() {
		// Default to the past so a test's captured "now" always sees it available.
		r.AvailableAt = now.Add(-time.Hour)
	}
	r.CreatedAt, r.UpdatedAt = now.Add(-time.Hour), now.Add(-time.Hour)
	if err := s.EnqueueRun(context.Background(), r); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return r
}

func TestSQLStore_DequeueLeasesAndOrders(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()

	// Higher priority should be claimed first.
	mustEnqueue(t, s, &Run{ID: "low", SessionID: "s1", Priority: 1})
	mustEnqueue(t, s, &Run{ID: "high", SessionID: "s2", Priority: 10})

	got, err := s.DequeueRun(ctx, "w1", time.Minute, now)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got.ID != "high" {
		t.Fatalf("want high priority run first, got %q", got.ID)
	}
	if got.Status != StatusLeased || got.LeaseOwner != "w1" {
		t.Fatalf("run not leased to w1: %+v", got)
	}
	if got.Attempts != 1 {
		t.Fatalf("attempts want 1 got %d", got.Attempts)
	}
	if got.LeaseExpiresAt == nil || !got.LeaseExpiresAt.After(now) {
		t.Fatalf("lease expiry not set: %+v", got.LeaseExpiresAt)
	}
}

func TestSQLStore_DequeueEmptyAndScheduled(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()

	// Scheduled in the future: not yet claimable (durable timer).
	mustEnqueue(t, s, &Run{ID: "future", SessionID: "s1", AvailableAt: now.Add(time.Hour)})

	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty for future run, got %v", err)
	}
	// After its time, it becomes claimable.
	got, err := s.DequeueRun(ctx, "w1", time.Minute, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("dequeue after time: %v", err)
	}
	if got.ID != "future" {
		t.Fatalf("want future run, got %q", got.ID)
	}
}

func TestSQLStore_HeartbeatAndComplete(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1"})
	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
		t.Fatalf("dequeue: %v", err)
	}

	tests := []struct {
		name    string
		owner   string
		wantErr error
	}{
		{"owner heartbeats", "w1", nil},
		{"stranger cannot heartbeat", "w2", ErrLeaseLost},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Heartbeat(ctx, "r1", tc.owner, time.Minute, now.Add(time.Second))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("heartbeat err = %v, want %v", err, tc.wantErr)
			}
		})
	}

	if err := s.CompleteRun(ctx, "r1", "w2", StatusCompleted, "", now); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("complete by stranger: want ErrLeaseLost got %v", err)
	}
	if err := s.CompleteRun(ctx, "r1", "w1", StatusCompleted, "", now); err != nil {
		t.Fatalf("complete by owner: %v", err)
	}
	got, err := s.GetRun(ctx, "r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusCompleted || got.LeaseOwner != "" {
		t.Fatalf("run not completed cleanly: %+v", got)
	}
}

func TestSQLStore_RescheduleSleep(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1"})
	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
		t.Fatalf("dequeue: %v", err)
	}

	wake := now.Add(30 * time.Minute)
	patch := []byte(`{"resume":true}`)
	if err := s.RescheduleRun(ctx, "r1", "w1", wake, patch, now); err != nil {
		t.Fatalf("reschedule: %v", err)
	}

	// Not claimable before wake time.
	if _, err := s.DequeueRun(ctx, "w2", time.Minute, now.Add(time.Minute)); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty before wake, got %v", err)
	}
	got, err := s.DequeueRun(ctx, "w2", time.Minute, wake.Add(time.Second))
	if err != nil {
		t.Fatalf("dequeue after wake: %v", err)
	}
	if !bytes.Equal(got.Payload, patch) {
		t.Fatalf("payload not patched: %q", got.Payload)
	}
}

func TestSQLStore_ParkThenSignal(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1"})
	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if err := s.ParkRun(ctx, "r1", "w1", "approval", nil, now); err != nil {
		t.Fatalf("park: %v", err)
	}
	// Parked runs are not claimable.
	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty while parked, got %v", err)
	}

	n, err := s.DeliverSignal(ctx, &Signal{SessionID: "s1", Name: "approval", Payload: []byte("go")}, now)
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 awakened, got %d", n)
	}
	got, err := s.DequeueRun(ctx, "w2", time.Minute, now)
	if err != nil {
		t.Fatalf("dequeue after signal: %v", err)
	}
	if string(got.SignalPayload) != "go" {
		t.Fatalf("signal payload not delivered: %q", got.SignalPayload)
	}
}

func TestSQLStore_SignalBeforePark(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1"})
	if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	// Signal arrives before the run parks; it must be retained.
	n, err := s.DeliverSignal(ctx, &Signal{SessionID: "s1", Name: "approval", Payload: []byte("early")}, now)
	if err != nil {
		t.Fatalf("signal: %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 awakened (none parked yet), got %d", n)
	}
	// Parking now must observe the retained signal and stay claimable.
	if err = s.ParkRun(ctx, "r1", "w1", "approval", nil, now); err != nil {
		t.Fatalf("park: %v", err)
	}
	got, err := s.DequeueRun(ctx, "w2", time.Minute, now)
	if err != nil {
		t.Fatalf("dequeue after retained signal: %v", err)
	}
	if string(got.SignalPayload) != "early" {
		t.Fatalf("retained signal payload wrong: %q", got.SignalPayload)
	}
}

func TestSQLStore_RecoverOrphans(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()

	// One recoverable (attempts < max), one exhausted (attempts >= max).
	mustEnqueue(t, s, &Run{ID: "recoverable", SessionID: "s1", MaxAttempts: 5})
	mustEnqueue(t, s, &Run{ID: "exhausted", SessionID: "s2", MaxAttempts: 1})
	if _, err := s.DequeueRun(ctx, "dead", time.Second, now); err != nil {
		t.Fatalf("dequeue 1: %v", err)
	}
	if _, err := s.DequeueRun(ctx, "dead", time.Second, now); err != nil {
		t.Fatalf("dequeue 2: %v", err)
	}

	// Advance past lease expiry.
	future := now.Add(time.Hour)
	recovered, err := s.RecoverOrphans(ctx, future)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("want 1 recovered, got %d", recovered)
	}
	rec, _ := s.GetRun(ctx, "recoverable")
	if rec.Status != StatusPending {
		t.Fatalf("recoverable not re-enqueued: %s", rec.Status)
	}
	exh, _ := s.GetRun(ctx, "exhausted")
	if exh.Status != StatusFailed {
		t.Fatalf("exhausted not failed: %s", exh.Status)
	}
}

func TestSQLStore_Idempotency(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()

	fresh, err := s.MarkIdempotent(ctx, "k1", now)
	if err != nil || !fresh {
		t.Fatalf("first mark: fresh=%v err=%v", fresh, err)
	}
	fresh, err = s.MarkIdempotent(ctx, "k1", now)
	if err != nil {
		t.Fatalf("second mark: %v", err)
	}
	if fresh {
		t.Fatalf("second mark should not be fresh")
	}
}

func TestSQLStore_OutboxDedupeAndDrain(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()

	e := &OutboxEntry{IdempotencyKey: "effect-1", Topic: "email", Payload: []byte("hi")}
	if err := s.EnqueueOutbox(ctx, e); err != nil {
		t.Fatalf("enqueue outbox: %v", err)
	}
	// Duplicate key is ignored (no error, no second row).
	if err := s.EnqueueOutbox(ctx, &OutboxEntry{IdempotencyKey: "effect-1", Topic: "email"}); err != nil {
		t.Fatalf("dup enqueue outbox: %v", err)
	}
	entries, err := s.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 pending entry, got %d", len(entries))
	}
	if err = s.MarkOutboxSent(ctx, entries[0].ID, now); err != nil {
		t.Fatalf("mark sent: %v", err)
	}
	entries, err = s.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("claim 2: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 pending after send, got %d", len(entries))
	}
}

func TestSQLStore_CountByStatus(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mustEnqueue(t, s, &Run{ID: "a", SessionID: "s"})
	mustEnqueue(t, s, &Run{ID: "b", SessionID: "s"})
	n, err := s.CountByStatus(ctx, StatusPending)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 pending, got %d", n)
	}
	if _, err := s.GetRun(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
