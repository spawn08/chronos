package a2a

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
)

// ErrTaskNotFound is returned by a TaskStore when a task id is unknown or is not
// visible to the caller's tenant. The server maps it to HTTP 404.
var ErrTaskNotFound = errors.New("a2a: task not found")

// TaskStore persists A2A tasks and drives their execution. The Server delegates
// every task-lifecycle operation to a TaskStore so the backend — the in-memory
// default or the durable, restart-resumable queue (see NewDurableStore) — is
// swappable without touching the HTTP layer.
//
// Implementations must be safe for concurrent use and honor the context tenant
// (see storage.WithTenant): a Get/Cancel for a task created under another tenant
// must return ErrTaskNotFound.
type TaskStore interface {
	// Submit persists a new task and begins (or schedules) its execution,
	// returning a snapshot of the created task.
	Submit(ctx context.Context, input string, metadata map[string]any) (*Task, error)
	// Get returns a snapshot of the task, or ErrTaskNotFound.
	Get(ctx context.Context, id string) (*Task, error)
	// Cancel transitions a pending or running task to the canceled state and
	// returns a snapshot. Terminal tasks are returned unchanged.
	//
	// Backends differ in whether Cancel preempts in-flight work: the in-memory
	// store aborts the handler via context cancellation; the durable store records
	// the cancellation and discards the handler's result when it finishes, but does
	// not interrupt a running handler.
	Cancel(ctx context.Context, id string) (*Task, error)
}

// snapshot returns a value copy of the task with its Metadata map deep-copied, so
// a snapshot handed to a caller (or to a handler as its working copy) never shares
// mutable map state with the live task under the store's lock.
func (t *Task) snapshot() Task {
	cp := *t
	if t.Metadata != nil {
		m := make(map[string]any, len(t.Metadata))
		for k, v := range t.Metadata {
			m[k] = v
		}
		cp.Metadata = m
	}
	return cp
}

// defaultWatchPoll is the interval at which watch polls a task for changes.
const defaultWatchPoll = 100 * time.Millisecond

// isTerminal reports whether a status is final (no further transitions).
func isTerminal(s TaskStatus) bool {
	switch s {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	}
	return false
}

// watch polls store for a task and emits a snapshot whenever its status or
// output changes, until the task reaches a terminal state or ctx is done. The
// returned channel is closed when streaming ends. A single poll-based helper
// serves both store backends, so the server's SSE handler is backend-agnostic.
func watch(ctx context.Context, store TaskStore, id string, interval time.Duration) <-chan Task {
	if interval <= 0 {
		interval = defaultWatchPoll
	}
	out := make(chan Task, 1)
	go func() {
		defer close(out)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var last TaskStatus
		var lastOut string
		seen := false
		// emit reports whether streaming should continue.
		emit := func() bool {
			t, err := store.Get(ctx, id)
			if err != nil {
				return false
			}
			if !seen || t.Status != last || t.Output != lastOut {
				seen, last, lastOut = true, t.Status, t.Output
				select {
				case out <- *t:
				case <-ctx.Done():
					return false
				}
			}
			return !isTerminal(t.Status)
		}

		if !emit() {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !emit() {
					return
				}
			}
		}
	}()
	return out
}

// memStore is the default in-memory TaskStore. Tasks live only for the process
// lifetime; use NewDurableStore for restart-resumable tasks. It preserves the
// original Server behavior: Submit runs the handler in a goroutine. Tasks are
// partitioned by the context tenant so it is safe-by-default behind the
// tenant-scoped served endpoint — a task created under one tenant is invisible
// to another, matching the TaskStore contract.
//
// Note: terminal tasks are retained for the process lifetime (no TTL/eviction),
// so a long-lived server accumulates one record per task ever submitted. It is
// intended as the dev/default backend; use NewDurableStore for production.
type memStore struct {
	handler Handler
	mu      sync.Mutex
	tasks   map[string]*Task              // key: memKey(tenant, id)
	cancels map[string]context.CancelFunc // key: memKey(tenant, id)
	counter int64
}

// newMemStore builds an in-memory store that executes tasks with handler.
func newMemStore(handler Handler) *memStore {
	return &memStore{
		handler: handler,
		tasks:   make(map[string]*Task),
		cancels: make(map[string]context.CancelFunc),
	}
}

// memKey namespaces a task id by the context tenant so lookups never cross
// tenants. The NUL separator cannot appear in a tenant id or task id.
func memKey(ctx context.Context, id string) string {
	return storage.TenantFromContext(ctx) + "\x00" + id
}

func (m *memStore) Submit(ctx context.Context, input string, metadata map[string]any) (*Task, error) {
	// Detach cancellation from the request context (which ends when the HTTP
	// handler returns) but preserve its values (including the tenant).
	execCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))

	m.mu.Lock()
	m.counter++
	now := time.Now()
	task := &Task{
		ID:        fmt.Sprintf("task_%d", m.counter),
		Status:    TaskStatusPending,
		Input:     input,
		Metadata:  metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	key := memKey(ctx, task.ID)
	m.tasks[key] = task
	m.cancels[key] = cancel
	snapshot := task.snapshot()
	m.mu.Unlock()

	go m.execute(execCtx, task.ID)
	return &snapshot, nil
}

func (m *memStore) Get(ctx context.Context, id string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[memKey(ctx, id)]
	if !ok {
		return nil, ErrTaskNotFound
	}
	snapshot := task.snapshot()
	return &snapshot, nil
}

func (m *memStore) Cancel(ctx context.Context, id string) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memKey(ctx, id)
	task, ok := m.tasks[key]
	if !ok {
		return nil, ErrTaskNotFound
	}
	if task.Status == TaskStatusPending || task.Status == TaskStatusRunning {
		task.Status = TaskStatusCancelled
		task.UpdatedAt = time.Now()
		if c := m.cancels[key]; c != nil {
			c()
			delete(m.cancels, key)
		}
	}
	snapshot := task.snapshot()
	return &snapshot, nil
}

// execute runs the handler for a task and records its terminal state. The
// handler mutates a copy so a concurrent Get never observes a partial write. ctx
// carries the tenant (preserved from Submit) used to key the task.
func (m *memStore) execute(ctx context.Context, id string) {
	key := memKey(ctx, id)

	m.mu.Lock()
	task, ok := m.tasks[key]
	if !ok || task.Status == TaskStatusCancelled {
		m.mu.Unlock()
		return
	}
	task.Status = TaskStatusRunning
	task.UpdatedAt = time.Now()
	work := task.snapshot()
	m.mu.Unlock()

	err := m.handler(ctx, &work)

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cancels, key)
	task, ok = m.tasks[key]
	if !ok || task.Status == TaskStatusCancelled {
		return
	}
	task.Output = work.Output
	if work.Metadata != nil {
		task.Metadata = work.Metadata
	}
	task.UpdatedAt = time.Now()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
	} else {
		task.Status = TaskStatusCompleted
	}
}
