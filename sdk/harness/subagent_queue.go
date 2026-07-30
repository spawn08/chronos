package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage"
)

// SubAgentGraphID is the id of the shared, single-node graph that runs a
// registered subagent. Every worker compiles it over the SAME SubAgentService,
// so a subagent run enqueued on one node — or orphaned when a worker dies — can
// be executed by any other worker.
const SubAgentGraphID = "chronos.subagent"

// Reserved graph.State keys carrying the subagent invocation across the queue.
// The double-underscore convention keeps them clear of user state; they are the
// intentional coupling point between the harness and the shared graph.
const (
	stateSubAgentName  = "__subagent_name__"
	stateSubAgentTask  = "__subagent_task__"
	stateSubAgentDepth = "__subagent_depth__"
	stateSubAgentOut   = "__subagent_result__"
)

// NewSubAgentGraph builds the shared subagent-runner graph over svc. Wire it into
// a queue worker via graph.NewQueuedExecutor with a resolver that returns it for
// SubAgentGraphID; run the same wiring on every worker so orphaned subagent runs
// recover onto another node.
//
// Only registered subagents are runnable this way: a worker rebuilds the
// subagent from svc by name, so the definition must exist in every worker's
// service. Dynamic subagents (defined inline at spawn time) are not
// reconstructable remotely and must use InProcessRunner.
func NewSubAgentGraph(svc *SubAgentService) (*graph.CompiledGraph, error) {
	g := graph.New(SubAgentGraphID)
	g.AddNode("run", func(ctx context.Context, s graph.State) (graph.State, error) {
		name, _ := s[stateSubAgentName].(string)
		task, _ := s[stateSubAgentTask].(string)
		spec, ok := svc.lookup(name)
		if !ok {
			return nil, fmt.Errorf("harness: subagent graph: unknown registered subagent %q", name)
		}
		// Rehydrate the recursion depth carried across the queue boundary so a
		// nested spawn stays bounded on the durable path too.
		result, err := svc.run(withDepth(ctx, stateDepth(s)), spec, task)
		if err != nil {
			return nil, err // svc.run already wraps with harness context
		}
		s[stateSubAgentOut] = result
		return s, nil
	})
	g.SetEntryPoint("run")
	g.SetFinishPoint("run")
	compiled, err := g.Compile()
	if err != nil {
		return nil, fmt.Errorf("harness: compile subagent graph: %w", err)
	}
	return compiled, nil
}

// stateDepth reads the recursion depth from graph state, tolerating the float64
// that a number becomes after a JSON round-trip through the run payload.
func stateDepth(s graph.State) int {
	switch v := s[stateSubAgentDepth].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

// defaultPollInterval is how often QueuedRunner checks a run's status.
const defaultPollInterval = 50 * time.Millisecond

// QueuedRunner runs a subagent as a durable queued graph run: it enqueues the
// invocation and waits for a worker to complete it, reading the result from the
// run's final checkpoint. Because the work lives in the durable queue, killing
// the worker mid-run lets the reaper re-lease it to another worker, which
// completes it — the subagent is resumable and relocatable.
//
// It requires the subagent to be registered with the service (see
// NewSubAgentGraph); dynamic subagents are rejected because a remote worker
// cannot reconstruct them.
type QueuedRunner struct {
	svc     *SubAgentService
	q       *queue.Queue
	store   storage.Storage
	poll    time.Duration
	timeout time.Duration
}

// QueuedOption configures a QueuedRunner.
type QueuedOption func(*QueuedRunner)

// WithPollInterval sets how often the runner polls for completion. A value <= 0
// restores the default.
func WithPollInterval(d time.Duration) QueuedOption {
	return func(r *QueuedRunner) {
		if d <= 0 {
			d = defaultPollInterval
		}
		r.poll = d
	}
}

// WithTimeout bounds how long Run waits for a worker to complete the subagent.
// Without it (or with 0) Run waits until the caller's context is done — so a
// caller passing a non-cancelable context with no worker draining the queue
// would block forever; set a timeout in that case.
func WithTimeout(d time.Duration) QueuedOption {
	return func(r *QueuedRunner) { r.timeout = d }
}

// NewQueuedRunner creates a durable subagent runner over svc, the queue q, and
// the checkpoint store the workers use.
func NewQueuedRunner(svc *SubAgentService, q *queue.Queue, store storage.Storage, opts ...QueuedOption) *QueuedRunner {
	r := &QueuedRunner{svc: svc, q: q, store: store, poll: defaultPollInterval}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Run enqueues spec as a durable subagent run and blocks until a worker finishes
// it (or the context/timeout elapses), returning the subagent's result.
func (r *QueuedRunner) Run(ctx context.Context, spec SubAgentSpec, task string) (string, error) {
	if _, ok := r.svc.lookup(spec.Name); !ok {
		return "", fmt.Errorf("harness: queued runner requires a registered subagent, %q is not registered", spec.Name)
	}
	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	sessionID := fmt.Sprintf("subagent_%s_%d", spec.Name, time.Now().UnixNano())
	payload, err := json.Marshal(graph.RunPayload{Initial: graph.State{
		stateSubAgentName:  spec.Name,
		stateSubAgentTask:  task,
		stateSubAgentDepth: depthFromContext(ctx),
	}})
	if err != nil {
		return "", fmt.Errorf("harness: marshal subagent payload: %w", err)
	}

	run := &queue.Run{
		ID:        sessionID,
		SessionID: sessionID,
		GraphID:   SubAgentGraphID,
		Kind:      queue.KindStart,
		Payload:   payload,
	}
	if err := r.q.Enqueue(ctx, run); err != nil {
		return "", fmt.Errorf("harness: enqueue subagent run: %w", err)
	}

	return r.await(ctx, run.ID, sessionID)
}

// await polls the run to completion and extracts its result from the final
// checkpoint. The result lives in graph.State under stateSubAgentOut, written by
// the subagent-runner node and durably checkpointed by the graph runner.
func (r *QueuedRunner) await(ctx context.Context, runID, sessionID string) (string, error) {
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			got, err := r.q.Get(ctx, runID)
			if err != nil {
				return "", fmt.Errorf("harness: poll subagent run: %w", err)
			}
			switch got.Status {
			case queue.StatusCompleted:
				return r.result(ctx, sessionID)
			case queue.StatusFailed:
				return "", fmt.Errorf("harness: subagent run failed: %s", got.LastError)
			}
		}
	}
}

// result reads the subagent's output from the session's latest checkpoint,
// erroring when a run reports completion without a result rather than returning
// a silent empty string.
func (r *QueuedRunner) result(ctx context.Context, sessionID string) (string, error) {
	cp, err := r.store.GetLatestCheckpoint(storage.WithSession(ctx, sessionID), sessionID)
	if err != nil {
		return "", fmt.Errorf("harness: load subagent result: %w", err)
	}
	v, ok := cp.State[stateSubAgentOut]
	if !ok {
		return "", fmt.Errorf("harness: subagent run %q completed without a result", sessionID)
	}
	result, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("harness: subagent result has unexpected type %T", v)
	}
	return result, nil
}
