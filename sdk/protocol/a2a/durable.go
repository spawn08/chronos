package a2a

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage"
)

// DurableStore is a TaskStore backed by the durable queue (engine/queue) for
// restart-resumable scheduling and by checkpoint storage (storage.Storage) for
// the task record. Execution is driven by a queue.Worker wired to Executor, so a
// task whose worker dies is re-leased and resumed once its lease expires (orphan
// recovery), and tasks survive a process restart. The task record is a single
// checkpoint keyed by the task id (overwritten on each transition), which
// inherits storage's per-tenant scoping: a Get for another tenant's task
// resolves to ErrTaskNotFound.
type DurableStore struct {
	q       *queue.Queue
	store   storage.Storage
	handler Handler
	now     func() time.Time
}

// NewDurableStore builds a queue-backed TaskStore. Wire Executor into a
// queue.Worker (and run a queue.Reaper for orphan recovery) to execute tasks:
//
//	ds := a2a.NewDurableStore(q, store, handler)
//	w, _ := queue.NewWorker(q, ds.Executor, queue.WorkerConfig{ID: "a2a-worker-1"})
//	go w.Run(ctx)
//	srv := a2a.NewServerWithStore(card, ds)
func NewDurableStore(q *queue.Queue, store storage.Storage, handler Handler) *DurableStore {
	return &DurableStore{q: q, store: store, handler: handler, now: time.Now}
}

// durablePayload is the queue Run payload for an A2A task. It carries the tenant
// so the worker (whose context has no tenant) re-establishes the correct scope
// before touching the task's checkpoint.
type durablePayload struct {
	Input    string         `json:"input"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Tenant   string         `json:"tenant"`
}

// Submit persists a pending task record and enqueues a run for a worker to pick
// up. The task is scoped to the context tenant.
func (d *DurableStore) Submit(ctx context.Context, input string, metadata map[string]any) (*Task, error) {
	now := d.now()
	task := &Task{
		ID:        newTaskID(),
		Status:    TaskStatusPending,
		Input:     input,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := d.save(ctx, task); err != nil {
		return nil, fmt.Errorf("a2a submit: %w", err)
	}

	payload, err := json.Marshal(durablePayload{
		Input:    input,
		Metadata: metadata,
		Tenant:   storage.TenantFromContext(ctx),
	})
	if err != nil {
		return nil, fmt.Errorf("a2a submit: marshal payload: %w", err)
	}
	if err := d.q.Enqueue(ctx, &queue.Run{
		SessionID: task.ID,
		Kind:      queue.KindStart,
		Payload:   payload,
	}); err != nil {
		return nil, fmt.Errorf("a2a submit: enqueue: %w", err)
	}

	snapshot := *task
	return &snapshot, nil
}

// Get returns the task record for the context tenant, or ErrTaskNotFound.
func (d *DurableStore) Get(ctx context.Context, id string) (*Task, error) {
	cp, err := d.store.GetLatestCheckpoint(ctx, id)
	if err != nil {
		// A missing row — including a cross-tenant miss — is a not-found, matching
		// the control plane's treatment of GetLatestCheckpoint.
		return nil, ErrTaskNotFound
	}
	task, err := stateToTask(cp.State)
	if err != nil {
		return nil, fmt.Errorf("a2a get task %s: %w", id, err)
	}
	return task, nil
}

// Cancel marks a pending or running task canceled. The running worker observes
// the cancellation via a checkpoint reload and stops recording further state.
func (d *DurableStore) Cancel(ctx context.Context, id string) (*Task, error) {
	task, err := d.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if task.Status == TaskStatusPending || task.Status == TaskStatusRunning {
		task.Status = TaskStatusCancelled
		task.UpdatedAt = d.now()
		if err := d.save(ctx, task); err != nil {
			return nil, fmt.Errorf("a2a cancel: %w", err)
		}
	}
	return task, nil
}

// Executor performs the work for a claimed run. Wire it into a queue.Worker. It
// re-establishes the task's tenant, runs the handler, and records the terminal
// state. Returning a non-nil Result.Err lets the queue retry with backoff up to
// the run's MaxAttempts; the task is marked failed only once the budget is spent.
func (d *DurableStore) Executor(ctx context.Context, run *queue.Run) queue.Result {
	var p durablePayload
	if err := json.Unmarshal(run.Payload, &p); err != nil {
		return queue.Result{Err: fmt.Errorf("a2a executor: decode payload: %w", err)}
	}
	execCtx := storage.WithTenant(ctx, p.Tenant)

	task := d.load(execCtx, run.SessionID, p)
	// A task canceled (or already finished) before this attempt needs no work.
	if isTerminal(task.Status) {
		return queue.Result{}
	}

	task.Status = TaskStatusRunning
	task.UpdatedAt = d.now()
	if err := d.save(execCtx, task); err != nil {
		return queue.Result{Err: fmt.Errorf("a2a executor: save running: %w", err)}
	}

	work := *task
	handlerErr := d.handler(execCtx, &work)

	// Honor a cancellation that landed while the handler ran.
	if latest, err := d.Get(execCtx, run.SessionID); err == nil && latest.Status == TaskStatusCancelled {
		return queue.Result{}
	}

	task.Output = work.Output
	if work.Metadata != nil {
		task.Metadata = work.Metadata
	}
	task.UpdatedAt = d.now()

	if handlerErr != nil {
		task.Error = handlerErr.Error()
		// Mark failed only on the final attempt; otherwise leave it running so a
		// poller/stream sees the retry rather than a spurious terminal failure.
		if run.Attempts+1 >= run.MaxAttempts {
			task.Status = TaskStatusFailed
		}
		_ = d.save(execCtx, task)
		return queue.Result{Err: handlerErr}
	}

	task.Status = TaskStatusCompleted
	if err := d.save(execCtx, task); err != nil {
		return queue.Result{Err: fmt.Errorf("a2a executor: save completed: %w", err)}
	}
	return queue.Result{}
}

// load reads the task record, reconstructing a pending task from the run payload
// if the checkpoint is absent (e.g. lost across a restart with volatile storage).
func (d *DurableStore) load(ctx context.Context, id string, p durablePayload) *Task {
	if task, err := d.Get(ctx, id); err == nil {
		return task
	}
	now := d.now()
	return &Task{
		ID:        id,
		Status:    TaskStatusPending,
		Input:     p.Input,
		Metadata:  p.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// save writes the task record as a single checkpoint keyed by the task id. The
// adapter's INSERT-OR-REPLACE overwrites the prior state, so reads always see
// the latest transition and checkpoints do not accumulate per task.
func (d *DurableStore) save(ctx context.Context, task *Task) error {
	state, err := taskToState(task)
	if err != nil {
		return err
	}
	return d.store.SaveCheckpoint(ctx, &storage.Checkpoint{
		ID:        task.ID,
		SessionID: task.ID,
		NodeID:    "a2a",
		State:     state,
		CreatedAt: d.now(),
	})
}

// taskToState renders a task as a checkpoint state map via a JSON round-trip.
func taskToState(task *Task) (map[string]any, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode task state: %w", err)
	}
	return m, nil
}

// stateToTask reconstructs a task from a checkpoint state map.
func stateToTask(state map[string]any) (*Task, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode task state: %w", err)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal task: %w", err)
	}
	return &task, nil
}

// newTaskID returns a random, unguessable task id (also used as the session id
// for the task's checkpoint).
func newTaskID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("a2a_%d", time.Now().UnixNano())
	}
	return "a2a_" + hex.EncodeToString(b[:])
}
