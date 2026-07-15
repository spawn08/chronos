// Package queue provides a durable, distributed work queue for Chronos graph
// runs. It decouples run intake from execution: producers Enqueue runs and a
// pool of Workers claims them with a time-bounded lease, executes the graph,
// heartbeats to hold the lease, and re-enqueues in-flight runs abandoned by a
// dead worker once the lease expires.
//
// The queue is backed by a small Store interface (implemented by SQLStore for
// both Postgres and SQLite) so the durable state — the work queue, leases,
// durable timers, external signals, the idempotent outbox, and admission
// bookkeeping — survives process restarts and is shared across workers.
//
// Capabilities:
//   - Leased dequeue (Postgres FOR UPDATE SKIP LOCKED; SQLite atomic UPDATE).
//   - Heartbeat, lease expiry, and orphan recovery (P1-001, P1-002).
//   - Idempotency keys and a reliable outbox for external effects (P1-003).
//   - Durable timers/sleeps and external signals, including webhook-as-signal
//     for human-in-the-loop approval (P1-004).
//   - Global admission control / back-pressure with reject or park policies
//     (P1-005).
package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Run states.
const (
	StatusPending   = "pending"   // available (subject to AvailableAt) to be claimed
	StatusLeased    = "leased"    // claimed by a worker under an active lease
	StatusParked    = "parked"    // waiting on an external signal (HITL/back-pressure)
	StatusCompleted = "completed" // finished successfully
	StatusFailed    = "failed"    // failed terminally (attempts exhausted)
)

// Run kinds tell the executor how to start work for the run.
const (
	KindStart  = "start"  // begin a fresh graph execution from the payload
	KindResume = "resume" // resume an existing session from its latest checkpoint
)

// AdmissionParkSignal is the reserved signal name used to park runs shed by
// admission control; ReleaseAdmissionParked promotes them back to pending.
const AdmissionParkSignal = "__admission__"

// Errors returned by the queue.
var (
	// ErrEmpty is returned by Dequeue when no run is currently available.
	ErrEmpty = errors.New("queue: no available run")
	// ErrOverloaded is returned by Enqueue when admission control rejects the
	// run because the queue is at capacity.
	ErrOverloaded = errors.New("queue: overloaded, run rejected")
	// ErrLeaseLost indicates a heartbeat/complete failed because the caller no
	// longer holds the lease (it expired and was recovered by another worker).
	ErrLeaseLost = errors.New("queue: lease lost")
	// ErrNotFound is returned when a run does not exist.
	ErrNotFound = errors.New("queue: run not found")
)

// Run is a unit of durable work: one graph execution or resume.
type Run struct {
	ID             string     `json:"id"`
	SessionID      string     `json:"session_id"`
	GraphID        string     `json:"graph_id"`
	Kind           string     `json:"kind"`
	Payload        []byte     `json:"payload,omitempty"`
	Status         string     `json:"status"`
	Priority       int        `json:"priority"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LeaseOwner     string     `json:"lease_owner,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	AvailableAt    time.Time  `json:"available_at"`
	WaitSignal     string     `json:"wait_signal,omitempty"`
	SignalPayload  []byte     `json:"signal_payload,omitempty"`
	IdempotencyKey string     `json:"idempotency_key,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Signal is an external event delivered to parked runs of a session.
type Signal struct {
	SessionID string `json:"session_id"`
	Name      string `json:"name"`
	Payload   []byte `json:"payload,omitempty"`
}

// AdmissionPolicy controls what happens when the queue is at capacity.
type AdmissionPolicy string

const (
	// PolicyReject fails Enqueue with ErrOverloaded past MaxDepth.
	PolicyReject AdmissionPolicy = "reject"
	// PolicyPark parks runs past MaxDepth so they can be released later.
	PolicyPark AdmissionPolicy = "park"
)

// Config configures a Queue.
type Config struct {
	// MaxDepth bounds the number of pending runs. Zero disables admission
	// control (unbounded intake).
	MaxDepth int
	// Policy selects reject vs. park behavior at capacity.
	Policy AdmissionPolicy
	// DefaultMaxAttempts is applied to enqueued runs that do not set one.
	DefaultMaxAttempts int
}

// Queue is the high-level producer/administration API over a Store. It layers
// admission control on top of the durable Store operations. Workers consume via
// the same Queue.
type Queue struct {
	store Store
	cfg   Config
	now   func() time.Time
}

// New constructs a Queue over the given Store.
func New(store Store, cfg Config) *Queue {
	if cfg.Policy == "" {
		cfg.Policy = PolicyReject
	}
	if cfg.DefaultMaxAttempts <= 0 {
		cfg.DefaultMaxAttempts = 5
	}
	return &Queue{store: store, cfg: cfg, now: time.Now}
}

// Store exposes the underlying durable store (for advanced callers, e.g. the
// outbox drainer or orphan recovery loop).
func (q *Queue) Store() Store { return q.store }

// Migrate creates the queue's schema.
func (q *Queue) Migrate(ctx context.Context) error {
	if err := q.store.Migrate(ctx); err != nil {
		return fmt.Errorf("queue migrate: %w", err)
	}
	return nil
}

// Enqueue admits and persists a run. Admission control may reject (ErrOverloaded)
// or park the run under overload depending on the configured policy. The run's
// ID, timestamps, and default MaxAttempts are filled in when unset.
func (q *Queue) Enqueue(ctx context.Context, r *Run) error {
	if r == nil {
		return fmt.Errorf("enqueue: nil run")
	}
	now := q.now()
	if r.ID == "" {
		r.ID = fmt.Sprintf("run_%d_%s", now.UnixNano(), r.SessionID)
	}
	if r.Kind == "" {
		r.Kind = KindStart
	}
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = q.cfg.DefaultMaxAttempts
	}
	if r.AvailableAt.IsZero() {
		r.AvailableAt = now
	}
	r.CreatedAt, r.UpdatedAt = now, now
	r.Status = StatusPending

	parked, err := q.admit(ctx)
	if err != nil {
		return err
	}
	if parked {
		r.Status = StatusParked
		r.WaitSignal = AdmissionParkSignal
	}

	if err := q.store.EnqueueRun(ctx, r); err != nil {
		return fmt.Errorf("enqueue: %w", err)
	}
	return nil
}

// admit applies the configured admission policy. It returns (parked=true) when
// the run should be stored in the parked state, or ErrOverloaded when rejected.
func (q *Queue) admit(ctx context.Context) (bool, error) {
	if q.cfg.MaxDepth <= 0 {
		return false, nil
	}
	depth, err := q.store.CountByStatus(ctx, StatusPending)
	if err != nil {
		return false, fmt.Errorf("admit: count pending: %w", err)
	}
	if depth < q.cfg.MaxDepth {
		return false, nil
	}
	switch q.cfg.Policy {
	case PolicyPark:
		return true, nil
	default:
		return false, ErrOverloaded
	}
}

// ReleaseAdmissionParked promotes up to limit admission-parked runs back to
// pending, but only while there is spare capacity under MaxDepth. It returns the
// number of runs released.
func (q *Queue) ReleaseAdmissionParked(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 1
	}
	if q.cfg.MaxDepth > 0 {
		depth, err := q.store.CountByStatus(ctx, StatusPending)
		if err != nil {
			return 0, fmt.Errorf("release parked: count pending: %w", err)
		}
		if slack := q.cfg.MaxDepth - depth; slack < limit {
			limit = slack
		}
		if limit <= 0 {
			return 0, nil
		}
	}
	n, err := q.store.ReleaseParked(ctx, AdmissionParkSignal, limit, q.now())
	if err != nil {
		return 0, fmt.Errorf("release parked: %w", err)
	}
	return n, nil
}

// Dequeue claims the next available run under a lease held by owner. It returns
// ErrEmpty when nothing is available.
func (q *Queue) Dequeue(ctx context.Context, owner string, lease time.Duration) (*Run, error) {
	r, err := q.store.DequeueRun(ctx, owner, lease, q.now())
	if err != nil {
		if errors.Is(err, ErrEmpty) {
			return nil, ErrEmpty
		}
		return nil, fmt.Errorf("dequeue: %w", err)
	}
	return r, nil
}

// Heartbeat extends the lease on an in-flight run. Returns ErrLeaseLost if the
// caller no longer owns the run.
func (q *Queue) Heartbeat(ctx context.Context, runID, owner string, lease time.Duration) error {
	if err := q.store.Heartbeat(ctx, runID, owner, lease, q.now()); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

// Complete marks a leased run finished with the given terminal status.
func (q *Queue) Complete(ctx context.Context, runID, owner, status, lastErr string) error {
	if err := q.store.CompleteRun(ctx, runID, owner, status, lastErr, q.now()); err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	return nil
}

// Sleep durably reschedules a leased run to become available after delay. It is
// the primitive behind "wait N, then continue" that survives process restarts.
// Sleep does NOT consume retry budget (attempts is unchanged): durable timers
// and HITL parks are healthy redeliveries, not failures.
func (q *Queue) Sleep(ctx context.Context, runID, owner string, delay time.Duration, patch []byte) error {
	availableAt := q.now().Add(delay)
	if err := q.store.RescheduleRun(ctx, runID, owner, availableAt, patch, q.now()); err != nil {
		return fmt.Errorf("sleep: %w", err)
	}
	return nil
}

// Retry durably reschedules a leased run to become available after delay AND
// increments its attempts counter (the retry budget). It is the post-error
// backoff path; callers must decide separately when attempts are exhausted.
func (q *Queue) Retry(ctx context.Context, runID, owner string, delay time.Duration, patch []byte) error {
	availableAt := q.now().Add(delay)
	if err := q.store.RetryRun(ctx, runID, owner, availableAt, patch, q.now()); err != nil {
		return fmt.Errorf("retry: %w", err)
	}
	return nil
}

// Park suspends a leased run until an external signal named waitSignal is
// delivered for its session. If a matching signal was already delivered, the run
// is made immediately available instead (no lost-signal race).
func (q *Queue) Park(ctx context.Context, runID, owner, waitSignal string, patch []byte) error {
	if err := q.store.ParkRun(ctx, runID, owner, waitSignal, patch, q.now()); err != nil {
		return fmt.Errorf("park: %w", err)
	}
	return nil
}

// Signal delivers an external signal, waking any parked run of the session that
// is waiting on it. If no run is waiting yet, the signal is retained so a
// subsequent Park observes it. It returns the number of runs awakened. This is
// the entry point a webhook handler calls to resume a HITL run.
func (q *Queue) Signal(ctx context.Context, sig *Signal) (int, error) {
	if sig == nil || sig.SessionID == "" || sig.Name == "" {
		return 0, fmt.Errorf("signal: session_id and name are required")
	}
	n, err := q.store.DeliverSignal(ctx, sig, q.now())
	if err != nil {
		return 0, fmt.Errorf("signal: %w", err)
	}
	return n, nil
}

// RecoverOrphans re-enqueues leased runs whose lease has expired (their worker
// died). Retry-exhausted runs are failed. It returns the number of runs recovered.
func (q *Queue) RecoverOrphans(ctx context.Context) (int, error) {
	n, err := q.store.RecoverOrphans(ctx, q.now())
	if err != nil {
		return 0, fmt.Errorf("recover orphans: %w", err)
	}
	return n, nil
}

// PendingDepth reports the number of pending runs (for observability/back-pressure).
func (q *Queue) PendingDepth(ctx context.Context) (int, error) {
	n, err := q.store.CountByStatus(ctx, StatusPending)
	if err != nil {
		return 0, fmt.Errorf("pending depth: %w", err)
	}
	return n, nil
}

// Get returns a run by ID.
func (q *Queue) Get(ctx context.Context, id string) (*Run, error) {
	r, err := q.store.GetRun(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}
