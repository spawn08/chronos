package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Result is what an Executor reports after handling a run. Exactly one outcome
// applies, checked in this order: Err/failed → retry or fail; Sleep>0 → durable
// timer; ParkSignal set → wait for external signal; otherwise → completed.
type Result struct {
	// Err, if non-nil, marks the attempt failed; the worker retries (with
	// backoff) until MaxAttempts, then fails the run terminally.
	Err error
	// Sleep, if > 0, durably reschedules the run to run again after the delay.
	Sleep time.Duration
	// ParkSignal, if set, parks the run until that signal is delivered.
	ParkSignal string
	// Patch, if non-nil, replaces the run's persisted payload on sleep/park so
	// the resumed attempt observes updated state.
	Patch []byte
}

// Executor performs the work for a claimed run. Implementations must respect
// ctx cancellation: the worker cancels ctx if the lease is lost mid-execution.
type Executor func(ctx context.Context, r *Run) Result

// WorkerConfig configures a Worker.
type WorkerConfig struct {
	// ID uniquely identifies this worker; it owns the leases it claims.
	ID string
	// Lease is the lease TTL granted on claim and extended by each heartbeat.
	Lease time.Duration
	// Heartbeat is how often the worker extends the lease while executing.
	Heartbeat time.Duration
	// PollInterval is how long to wait after an empty dequeue before retrying.
	PollInterval time.Duration
	// Backoff computes the retry delay from the attempt count. If nil, a bounded
	// exponential backoff is used.
	Backoff func(attempt int) time.Duration
}

func (c *WorkerConfig) withDefaults() {
	if c.Lease <= 0 {
		c.Lease = 30 * time.Second
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = c.Lease / 3
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 100 * time.Millisecond
	}
	if c.Backoff == nil {
		c.Backoff = defaultBackoff
	}
}

func defaultBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Duration(1<<min(attempt-1, 6)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// Worker claims runs from a Queue and executes them, holding the lease with
// periodic heartbeats and durably rescheduling/parking/failing per the Result.
type Worker struct {
	q    *Queue
	exec Executor
	cfg  WorkerConfig
}

// NewWorker constructs a Worker. It panics only on programmer error (nil queue or
// executor); all runtime failures are returned as errors.
func NewWorker(q *Queue, exec Executor, cfg WorkerConfig) (*Worker, error) {
	if q == nil {
		return nil, fmt.Errorf("new worker: nil queue")
	}
	if exec == nil {
		return nil, fmt.Errorf("new worker: nil executor")
	}
	if cfg.ID == "" {
		return nil, fmt.Errorf("new worker: empty worker ID")
	}
	cfg.withDefaults()
	return &Worker{q: q, exec: exec, cfg: cfg}, nil
}

// Run drives the claim/execute loop until ctx is canceled.
func (w *Worker) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		processed, err := w.RunOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			// Transient store error: back off and keep serving.
			if !sleepCtx(ctx, w.cfg.PollInterval) {
				return nil
			}
			continue
		}
		if !processed {
			if !sleepCtx(ctx, w.cfg.PollInterval) {
				return nil
			}
		}
	}
}

// RunOnce claims and processes at most one run. It returns processed=false when
// the queue is empty.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	r, err := w.q.Dequeue(ctx, w.cfg.ID, w.cfg.Lease)
	if err != nil {
		if errors.Is(err, ErrEmpty) {
			return false, nil
		}
		return false, err
	}
	w.process(ctx, r)
	return true, nil
}

// process executes one claimed run under a heartbeated lease and applies the
// result durably.
func (w *Worker) process(ctx context.Context, r *Run) {
	execCtx, cancelExec := context.WithCancel(ctx)
	defer cancelExec()

	stopHB := make(chan struct{})
	leaseLost := make(chan struct{})
	go w.heartbeat(execCtx, r, cancelExec, stopHB, leaseLost)

	res := w.exec(execCtx, r)

	close(stopHB)

	// If the lease was lost, another worker owns the run now; do not touch it.
	select {
	case <-leaseLost:
		return
	default:
	}

	w.applyResult(ctx, r, res)
}

// heartbeat extends the lease until stopHB is closed or the lease is lost. On
// lease loss it cancels execution and signals via leaseLost.
func (w *Worker) heartbeat(ctx context.Context, r *Run, cancelExec context.CancelFunc, stopHB <-chan struct{}, leaseLost chan<- struct{}) {
	t := time.NewTicker(w.cfg.Heartbeat)
	defer t.Stop()
	for {
		select {
		case <-stopHB:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.q.Heartbeat(ctx, r.ID, w.cfg.ID, w.cfg.Lease); err != nil {
				close(leaseLost)
				cancelExec()
				return
			}
		}
	}
}

// applyResult persists the outcome of an attempt. ErrLeaseLost is tolerated: it
// means the run was recovered by another worker and must not be double-finished.
func (w *Worker) applyResult(ctx context.Context, r *Run, res Result) {
	// Use a detached context so bookkeeping still lands if the parent ctx is
	// being torn down after a canceled execution.
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	var err error
	switch {
	case res.Err != nil:
		if r.Attempts >= r.MaxAttempts {
			err = w.q.Complete(bg, r.ID, w.cfg.ID, StatusFailed, res.Err.Error())
		} else {
			err = w.q.Sleep(bg, r.ID, w.cfg.ID, w.cfg.Backoff(r.Attempts), res.Patch)
		}
	case res.Sleep > 0:
		err = w.q.Sleep(bg, r.ID, w.cfg.ID, res.Sleep, res.Patch)
	case res.ParkSignal != "":
		err = w.q.Park(bg, r.ID, w.cfg.ID, res.ParkSignal, res.Patch)
	default:
		err = w.q.Complete(bg, r.ID, w.cfg.ID, StatusCompleted, "")
	}
	_ = tolerateLeaseLost(err)
}

func tolerateLeaseLost(err error) error {
	if err == nil || errors.Is(err, ErrLeaseLost) {
		return nil
	}
	return err
}

// sleepCtx sleeps for d or until ctx is done. It returns false if ctx ended.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Reaper periodically recovers orphaned runs (whose worker died) by re-enqueuing
// expired leases. Run it on one or more nodes; recovery is idempotent.
type Reaper struct {
	q        *Queue
	interval time.Duration
}

// NewReaper constructs a Reaper that scans every interval.
func NewReaper(q *Queue, interval time.Duration) *Reaper {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Reaper{q: q, interval: interval}
}

// Run scans for orphaned runs until ctx is canceled.
func (rp *Reaper) Run(ctx context.Context) error {
	t := time.NewTicker(rp.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if _, err := rp.q.RecoverOrphans(ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				// Keep scanning despite transient errors.
				continue
			}
		}
	}
}
