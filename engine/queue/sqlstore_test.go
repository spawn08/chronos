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
	// A claim is a delivery, not a failed attempt: attempts must stay 0 so
	// durable sleeps/parks do not burn the retry budget.
	if got.Attempts != 0 {
		t.Fatalf("attempts want 0 after claim got %d", got.Attempts)
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
	// A lost lease is a failed attempt: recovery must charge the retry budget.
	if rec.Attempts != 1 {
		t.Fatalf("recoverable attempts want 1 got %d", rec.Attempts)
	}
	exh, _ := s.GetRun(ctx, "exhausted")
	if exh.Status != StatusFailed {
		t.Fatalf("exhausted not failed: %s", exh.Status)
	}
}

// TestSQLStore_RetryVsReschedule proves the attempts semantics: RetryRun (the
// post-error path) burns retry budget while RescheduleRun (durable sleep) does
// not. attempts counts only failed attempts.
func TestSQLStore_RetryVsReschedule(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1", MaxAttempts: 5})

	// Sleep (reschedule) several times: attempts must stay 0.
	for i := 0; i < 3; i++ {
		if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
			t.Fatalf("dequeue %d: %v", i, err)
		}
		if err := s.RescheduleRun(ctx, "r1", "w1", now, nil, now); err != nil {
			t.Fatalf("reschedule %d: %v", i, err)
		}
	}
	got, _ := s.GetRun(ctx, "r1")
	if got.Attempts != 0 {
		t.Fatalf("attempts after 3 sleeps want 0 got %d", got.Attempts)
	}

	// Retry twice: attempts must climb to 2.
	for i := 0; i < 2; i++ {
		if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
			t.Fatalf("dequeue retry %d: %v", i, err)
		}
		if err := s.RetryRun(ctx, "r1", "w1", now, nil, now); err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
	}
	got, _ = s.GetRun(ctx, "r1")
	if got.Attempts != 2 {
		t.Fatalf("attempts after 2 retries want 2 got %d", got.Attempts)
	}
}

// TestSQLStore_ParkSignalOrdering documents the park/deliver ordering guarantee
// that FINDING Q01 hardens for Postgres. Under SQLite (serialized writers) both
// interleavings are already correct; the Postgres FOR UPDATE lock reproduces the
// same guarantee. Both orderings must leave the run runnable, never stranded.
func TestSQLStore_ParkSignalOrdering(t *testing.T) {
	tests := []struct {
		name         string
		signalFirst  bool // deliver the signal before the run parks
		wantAwakened int
	}{
		{"park then signal wakes it", false, 1},
		{"signal then park consumes retained", true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			ctx := context.Background()
			now := time.Now()
			mustEnqueue(t, s, &Run{ID: "r1", SessionID: "s1"})
			if _, err := s.DequeueRun(ctx, "w1", time.Minute, now); err != nil {
				t.Fatalf("dequeue: %v", err)
			}

			deliver := func() int {
				n, err := s.DeliverSignal(ctx, &Signal{SessionID: "s1", Name: "go", Payload: []byte("p")}, now)
				if err != nil {
					t.Fatalf("deliver: %v", err)
				}
				return n
			}
			park := func() {
				if err := s.ParkRun(ctx, "r1", "w1", "go", nil, now); err != nil {
					t.Fatalf("park: %v", err)
				}
			}

			if tc.signalFirst {
				if n := deliver(); n != tc.wantAwakened {
					t.Fatalf("awakened=%d want %d", n, tc.wantAwakened)
				}
				park()
			} else {
				park()
				if n := deliver(); n != tc.wantAwakened {
					t.Fatalf("awakened=%d want %d", n, tc.wantAwakened)
				}
			}

			// Regardless of ordering the run must be runnable, not stranded parked.
			got, err := s.DequeueRun(ctx, "w2", time.Minute, now)
			if err != nil {
				t.Fatalf("run stranded (not runnable after park+signal): %v", err)
			}
			if string(got.SignalPayload) != "p" {
				t.Fatalf("signal payload not delivered: %q", got.SignalPayload)
			}
		})
	}
}

// TestSQLStore_OutboxDeadLetter proves a permanently-failing entry is
// dead-lettered once attempts reach the cap and is no longer returned by
// ClaimOutbox (FINDING Q05), so it cannot starve newer effects.
func TestSQLStore_OutboxDeadLetter(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now()
	if err := s.EnqueueOutbox(ctx, &OutboxEntry{IdempotencyKey: "poison", Topic: "webhook"}); err != nil {
		t.Fatalf("enqueue outbox: %v", err)
	}

	const capAttempts = 3
	for i := 0; i < capAttempts; i++ {
		entries, err := s.ClaimOutbox(ctx, 10)
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if len(entries) != 1 {
			t.Fatalf("attempt %d: want 1 claimable entry, got %d", i, len(entries))
		}
		if err := s.MarkOutboxFailed(ctx, entries[0].ID, "boom", capAttempts, now); err != nil {
			t.Fatalf("mark failed %d: %v", i, err)
		}
	}

	// After the cap it is dead-lettered: no longer claimable.
	entries, err := s.ClaimOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("claim after cap: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 claimable after dead-letter, got %d", len(entries))
	}
	// And its terminal status is OutboxFailed.
	var status string
	var attempts int
	if err := s.db.QueryRowContext(ctx,
		`SELECT status, attempts FROM queue_outbox WHERE idempotency_key='poison'`).Scan(&status, &attempts); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if status != OutboxFailed || attempts != capAttempts {
		t.Fatalf("want status=%s attempts=%d, got status=%s attempts=%d", OutboxFailed, capAttempts, status, attempts)
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
