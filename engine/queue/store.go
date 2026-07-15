package queue

import (
	"context"
	"time"
)

// OutboxEntry is an external effect recorded transactionally so it can be
// delivered exactly once by the drainer, even across retries and resumes. The
// IdempotencyKey is unique: enqueuing the same effect twice is a no-op (P1-003).
type OutboxEntry struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Topic          string     `json:"topic"`
	Payload        []byte     `json:"payload,omitempty"`
	Status         string     `json:"status"` // pending, sent, failed
	Attempts       int        `json:"attempts"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
}

// Outbox entry states.
const (
	OutboxPending = "pending"
	OutboxSent    = "sent"
	OutboxFailed  = "failed"
)

// Store is the minimal durable persistence contract the queue depends on. It is
// defined here (rather than in the shared storage package) so the queue owns its
// own schema and can be backed by any *sql.DB via SQLStore. All timestamps are
// passed explicitly so behavior is deterministic and testable.
type Store interface {
	// Migrate creates the queue schema (idempotent).
	Migrate(ctx context.Context) error

	// EnqueueRun inserts a run.
	EnqueueRun(ctx context.Context, r *Run) error

	// DequeueRun atomically claims the highest-priority available run for owner,
	// setting a lease that expires at now+lease. Returns ErrEmpty if none.
	DequeueRun(ctx context.Context, owner string, lease time.Duration, now time.Time) (*Run, error)

	// Heartbeat extends the lease on a run still owned by owner. Returns
	// ErrLeaseLost if the run is no longer leased to owner.
	Heartbeat(ctx context.Context, runID, owner string, lease time.Duration, now time.Time) error

	// CompleteRun sets a leased run to a terminal status. Returns ErrLeaseLost if
	// owner no longer holds the lease.
	CompleteRun(ctx context.Context, runID, owner, status, lastErr string, now time.Time) error

	// RescheduleRun returns a leased run to pending, available at availableAt,
	// optionally replacing its payload (durable sleep / retry backoff).
	RescheduleRun(ctx context.Context, runID, owner string, availableAt time.Time, patch []byte, now time.Time) error

	// ParkRun suspends a leased run pending a signal named waitSignal. If a
	// matching signal was already delivered for the session, the run is made
	// available immediately instead of parked (no lost-signal race).
	ParkRun(ctx context.Context, runID, owner, waitSignal string, patch []byte, now time.Time) error

	// DeliverSignal wakes parked runs of the session waiting on sig.Name and
	// returns the count awakened. If none are waiting, the signal is retained.
	DeliverSignal(ctx context.Context, sig *Signal, now time.Time) (int, error)

	// ReleaseParked promotes up to limit runs parked on waitSignal to pending.
	ReleaseParked(ctx context.Context, waitSignal string, limit int, now time.Time) (int, error)

	// RecoverOrphans re-enqueues runs whose lease expired (worker died); runs
	// past MaxAttempts are failed. Returns the number of runs recovered.
	RecoverOrphans(ctx context.Context, now time.Time) (int, error)

	// CountByStatus returns the number of runs in the given status.
	CountByStatus(ctx context.Context, status string) (int, error)

	// GetRun returns a run by ID (ErrNotFound if absent).
	GetRun(ctx context.Context, id string) (*Run, error)

	// MarkIdempotent records key; fresh is true only for the first observation.
	MarkIdempotent(ctx context.Context, key string, now time.Time) (fresh bool, err error)

	// EnqueueOutbox records an external effect. Duplicate IdempotencyKeys are
	// ignored (returns nil without inserting).
	EnqueueOutbox(ctx context.Context, e *OutboxEntry) error

	// ClaimOutbox returns up to limit pending outbox entries.
	ClaimOutbox(ctx context.Context, limit int) ([]*OutboxEntry, error)

	// MarkOutboxSent marks an entry delivered.
	MarkOutboxSent(ctx context.Context, id string, now time.Time) error

	// MarkOutboxFailed records a delivery failure (entry stays pending for retry).
	MarkOutboxFailed(ctx context.Context, id, errMsg string, now time.Time) error

	// Close releases resources owned by the store.
	Close() error
}
