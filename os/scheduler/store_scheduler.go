package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Store is the durable persistence contract for the store-backed scheduler. It
// is defined here (rather than in the shared storage package) so the scheduler
// owns its own schema and can be backed by any *sql.DB via SQLStore. Timestamps
// are passed explicitly so behavior is deterministic and testable.
type Store interface {
	// Migrate creates the scheduler schema (idempotent).
	Migrate(ctx context.Context) error
	// Add persists a new schedule.
	Add(ctx context.Context, sched *Schedule) error
	// Remove deletes a schedule by ID.
	Remove(ctx context.Context, id string) error
	// Get returns a schedule by ID.
	Get(ctx context.Context, id string) (*Schedule, error)
	// List returns all schedules.
	List(ctx context.Context) ([]*Schedule, error)
	// ClaimDue atomically advances the NextRunAt of every schedule that is due at
	// now and returns only those this caller claimed. Across replicas sharing the
	// store, a given due firing is claimed by exactly one caller. nextFn computes
	// the next fire time for a cron expression strictly after the given instant.
	ClaimDue(ctx context.Context, now time.Time, nextFn func(expr string, after time.Time) time.Time) ([]*Schedule, error)
	// SetSession persists the reused session ID for a schedule.
	SetSession(ctx context.Context, id, sessionID string) error
	// AddRunRecord appends a run record to a schedule's history.
	AddRunRecord(ctx context.Context, rec RunRecord) error
	// History returns run records for a schedule.
	History(ctx context.Context, scheduleID string) ([]RunRecord, error)
	// Close releases resources owned by the store.
	Close() error
}

// StoreScheduler is a store-backed Scheduler that claims each due schedule
// exactly once across all replicas sharing the Store, via an atomic conditional
// advance of NextRunAt (SELECT ... FOR UPDATE SKIP LOCKED on Postgres; SQLite
// serializes writers), so two replicas polling the same store never both claim
// the same firing. Note this is exactly-once *claiming* / at-most-once
// *execution*: a crash after the claim commits but before runFn completes drops
// that one firing (it is not retried), rather than risking a double-run.
//
// It implements Runner. The Runner methods match the in-process Scheduler's
// signatures for drop-in substitution and therefore take no ctx; they use a
// background context internally. Callers that need ctx/error propagation should
// use the *Context variants (AddContext, ListContext, HistoryContext).
type StoreScheduler struct {
	store    Store
	runFn    RunFunc
	tick     time.Duration
	stopCh   chan struct{}
	stopOnce sync.Once
	counter  uint64
	now      func() time.Time
}

// NewStoreScheduler creates a store-backed Scheduler. runFn is called when a
// schedule fires. Call Migrate (or Start, which migrates) before use.
func NewStoreScheduler(store Store, runFn RunFunc) *StoreScheduler {
	return &StoreScheduler{
		store:  store,
		runFn:  runFn,
		tick:   time.Minute,
		stopCh: make(chan struct{}),
		now:    time.Now,
	}
}

// WithTickInterval sets the polling interval (for testing). Default is 1 minute.
func (s *StoreScheduler) WithTickInterval(d time.Duration) *StoreScheduler {
	s.tick = d
	return s
}

// Migrate creates the scheduler schema.
func (s *StoreScheduler) Migrate(ctx context.Context) error {
	if err := s.store.Migrate(ctx); err != nil {
		return fmt.Errorf("scheduler: migrate: %w", err)
	}
	return nil
}

func (s *StoreScheduler) newID() string {
	return fmt.Sprintf("sched_%d_%d", s.now().UnixNano(), atomic.AddUint64(&s.counter, 1))
}

// AddContext creates a new schedule.
func (s *StoreScheduler) AddContext(ctx context.Context, agentID, cronExpr, input string, newSession bool) (*Schedule, error) {
	if err := validateCron(cronExpr); err != nil {
		return nil, fmt.Errorf("scheduler: invalid cron expression: %w", err)
	}
	now := s.now()
	sched := &Schedule{
		ID:         s.newID(),
		AgentID:    agentID,
		CronExpr:   cronExpr,
		Input:      input,
		NewSession: newSession,
		Enabled:    true,
		CreatedAt:  now,
		NextRunAt:  nextCronTime(cronExpr, now),
	}
	if err := s.store.Add(ctx, sched); err != nil {
		return nil, fmt.Errorf("scheduler: add: %w", err)
	}
	return sched, nil
}

// Add creates a new schedule (Runner interface; background context).
func (s *StoreScheduler) Add(agentID, cronExpr, input string, newSession bool) (*Schedule, error) {
	return s.AddContext(context.Background(), agentID, cronExpr, input, newSession)
}

// Remove deletes a schedule (Runner interface; background context).
func (s *StoreScheduler) Remove(id string) error {
	if err := s.store.Remove(context.Background(), id); err != nil {
		return fmt.Errorf("scheduler: remove: %w", err)
	}
	return nil
}

// Get returns a schedule by ID (Runner interface; background context).
func (s *StoreScheduler) Get(id string) (*Schedule, error) {
	sched, err := s.store.Get(context.Background(), id)
	if err != nil {
		return nil, fmt.Errorf("scheduler: get: %w", err)
	}
	return sched, nil
}

// ListContext returns all schedules.
func (s *StoreScheduler) ListContext(ctx context.Context) ([]*Schedule, error) {
	list, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list: %w", err)
	}
	return list, nil
}

// List returns all schedules (Runner interface). A store error yields an empty
// slice; use ListContext for error propagation.
func (s *StoreScheduler) List() []*Schedule {
	list, err := s.ListContext(context.Background())
	if err != nil {
		return nil
	}
	return list
}

// HistoryContext returns run records for a schedule.
func (s *StoreScheduler) HistoryContext(ctx context.Context, scheduleID string) ([]RunRecord, error) {
	recs, err := s.store.History(ctx, scheduleID)
	if err != nil {
		return nil, fmt.Errorf("scheduler: history: %w", err)
	}
	return recs, nil
}

// History returns run records for a schedule (Runner interface). A store error
// yields nil; use HistoryContext for error propagation.
func (s *StoreScheduler) History(scheduleID string) []RunRecord {
	recs, err := s.HistoryContext(context.Background(), scheduleID)
	if err != nil {
		return nil
	}
	return recs
}

// Start begins the scheduler loop, migrating the schema first, then checking for
// due schedules every tick. It returns when ctx is canceled or Stop is called.
func (s *StoreScheduler) Start(ctx context.Context) {
	if err := s.Migrate(ctx); err != nil {
		return
	}
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.Tick(ctx, now)
		}
	}
}

// Stop halts the scheduler loop.
func (s *StoreScheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Tick claims and fires all schedules due at now. It is exported so tests (and
// external drivers) can drive a single deterministic scheduling cycle.
func (s *StoreScheduler) Tick(ctx context.Context, now time.Time) {
	claimed, err := s.store.ClaimDue(ctx, now, nextCronTime)
	if err != nil {
		return
	}
	for _, sched := range claimed {
		s.fire(ctx, sched, now)
	}
}

// fire executes a claimed schedule and records its history. The schedule's
// NextRunAt/LastRunAt/RunCount were already advanced atomically by ClaimDue.
func (s *StoreScheduler) fire(ctx context.Context, sched *Schedule, now time.Time) {
	sessionID := sched.SessionID
	if sched.NewSession || sessionID == "" {
		sessionID = fmt.Sprintf("sched_%s_%d", sched.ID, s.now().UnixNano())
	}

	rec := RunRecord{
		ID:         fmt.Sprintf("run_%s_%d_%d", sched.ID, now.UnixNano(), atomic.AddUint64(&s.counter, 1)),
		ScheduleID: sched.ID,
		AgentID:    sched.AgentID,
		SessionID:  sessionID,
		Input:      sched.Input,
		StartedAt:  s.now(),
	}

	var runErr error
	if s.runFn != nil {
		runErr = s.runFn(ctx, sched.AgentID, sched.Input, sessionID)
	}
	rec.FinishedAt = s.now()
	if runErr != nil {
		rec.Status = "error"
		rec.Error = runErr.Error()
	} else {
		rec.Status = "success"
	}

	if !sched.NewSession {
		_ = s.store.SetSession(ctx, sched.ID, sessionID)
	}
	_ = s.store.AddRunRecord(ctx, rec)
}
