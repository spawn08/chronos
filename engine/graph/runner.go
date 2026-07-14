package graph

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
)

// DefaultMaxSteps bounds the number of node executions in a single run to guard
// against cyclic graphs looping forever (P0-006).
const DefaultMaxSteps = 1000

// CheckpointCommitter is an optional interface a storage.Storage implementation
// may satisfy to persist a checkpoint and its ledger event atomically in a single
// transaction. When the configured store implements it, the runner uses it so a
// crash between the two writes cannot desync the checkpoint from the event ledger.
// Stores that do not implement it fall back to two idempotent calls.
type CheckpointCommitter interface {
	SaveCheckpointAndEvent(ctx context.Context, cp *storage.Checkpoint, evt *storage.Event) error
}

// Runner executes a CompiledGraph with durable checkpointing.
//
// A Runner is single-use: each Run/Resume/Replay/Fork call must be made on a
// freshly constructed Runner. Reusing a Runner returns an error rather than
// panicking on a closed channel.
type Runner struct {
	graph       *CompiledGraph
	store       storage.Storage
	broker      *stream.Broker
	tracer      *trace.Collector
	localCh     chan StreamEvent
	maxSteps    int
	nodeTimeout time.Duration

	mu       sync.Mutex
	started  bool // guards single-use
	chClosed bool // guards emit against a send on a closed channel
}

// NewRunner creates a runner for the given compiled graph.
func NewRunner(g *CompiledGraph, store storage.Storage) *Runner {
	return &Runner{
		graph:    g,
		store:    store,
		localCh:  make(chan StreamEvent, 256),
		maxSteps: DefaultMaxSteps,
	}
}

// WithBroker attaches an SSE Broker so the runner publishes events to SSE subscribers.
func (r *Runner) WithBroker(b *stream.Broker) *Runner {
	r.broker = b
	return r
}

// WithTracer attaches a trace.Collector for span-based execution tracing.
func (r *Runner) WithTracer(t *trace.Collector) *Runner {
	r.tracer = t
	return r
}

// WithMaxSteps overrides the maximum number of node executions for a single run.
// A value <= 0 restores the default (DefaultMaxSteps).
func (r *Runner) WithMaxSteps(n int) *Runner {
	if n <= 0 {
		n = DefaultMaxSteps
	}
	r.maxSteps = n
	return r
}

// WithNodeTimeout bounds how long a single node may execute. Zero (the default)
// disables the per-node timeout.
func (r *Runner) WithNodeTimeout(d time.Duration) *Runner {
	r.nodeTimeout = d
	return r
}

// Stream returns a channel of execution events for real-time observability.
func (r *Runner) Stream() <-chan StreamEvent {
	return r.localCh
}

func (r *Runner) emit(evt StreamEvent) {
	evt.Timestamp = time.Now()
	r.mu.Lock()
	if !r.chClosed {
		select {
		case r.localCh <- evt:
		default:
		}
	}
	r.mu.Unlock()
	if r.broker != nil {
		r.broker.Publish(stream.Event{
			Type: evt.Type,
			Data: evt,
		})
	}
}

// closeLocalCh closes the local stream channel exactly once. Safe to call from
// any exit path via defer.
func (r *Runner) closeLocalCh() {
	r.mu.Lock()
	if !r.chClosed {
		r.chClosed = true
		close(r.localCh)
	}
	r.mu.Unlock()
}

// begin marks the runner as used. It returns an error if the runner has already
// executed, making reuse a clean error rather than a panic (P0-005).
func (r *Runner) begin() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return fmt.Errorf("runner: already used; construct a new Runner for each execution")
	}
	r.started = true
	return nil
}

// Run starts a new execution of the graph with the given initial state.
func (r *Runner) Run(ctx context.Context, sessionID string, initial State) (*RunState, error) {
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	rs := &RunState{
		RunID:       runID,
		SessionID:   sessionID,
		GraphID:     r.graph.ID,
		CurrentNode: r.graph.Entry,
		Status:      RunStatusRunning,
		State:       initial,
		SeqNum:      0,
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	return r.execute(ctx, rs, false)
}

// Resume continues execution from the latest checkpoint for the given session.
//
// The latest checkpoint records the *next* node to execute, so Resume never
// re-runs an already-completed node (P0-002). When that node is an interrupt
// node, Resume advances past it exactly once so an approved workflow proceeds
// instead of re-pausing forever (P0-001).
func (r *Runner) Resume(ctx context.Context, sessionID string) (*RunState, error) {
	cp, err := r.store.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("resume: no checkpoint found: %w", err)
	}

	rs := &RunState{
		RunID:       cp.RunID,
		SessionID:   sessionID,
		GraphID:     r.graph.ID,
		CurrentNode: cp.NodeID,
		Status:      RunStatusRunning,
		State:       State(cp.State),
		SeqNum:      cp.SeqNum,
		UpdatedAt:   time.Now(),
	}

	return r.execute(ctx, rs, true)
}

// ResumeFromCheckpoint resumes from a specific checkpoint (time-travel).
func (r *Runner) ResumeFromCheckpoint(ctx context.Context, checkpointID string) (*RunState, error) {
	cp, err := r.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("resume from checkpoint: %w", err)
	}

	rs := &RunState{
		RunID:       cp.RunID,
		SessionID:   cp.SessionID,
		GraphID:     r.graph.ID,
		CurrentNode: cp.NodeID,
		Status:      RunStatusRunning,
		State:       State(cp.State),
		SeqNum:      cp.SeqNum,
		UpdatedAt:   time.Now(),
	}

	return r.execute(ctx, rs, true)
}

// ForkFrom creates a new execution branch from a specific checkpoint with modified state.
// The original checkpoint history is preserved; the fork continues independently.
func (r *Runner) ForkFrom(ctx context.Context, checkpointID string, stateUpdate map[string]any) (*RunState, error) {
	cp, err := r.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("fork: checkpoint not found: %w", err)
	}

	forkedState := make(State, len(cp.State)+len(stateUpdate))
	for k, v := range cp.State {
		forkedState[k] = v
	}
	for k, v := range stateUpdate {
		forkedState[k] = v
	}

	forkSessionID := fmt.Sprintf("fork_%d", time.Now().UnixNano())

	if err := r.store.CreateSession(ctx, &storage.Session{
		ID:      forkSessionID,
		AgentID: "fork:" + cp.SessionID,
		Status:  "running",
		Metadata: map[string]any{
			"forked_from_checkpoint": checkpointID,
			"forked_from_session":    cp.SessionID,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}); err != nil {
		return nil, fmt.Errorf("fork: create session: %w", err)
	}

	rs := &RunState{
		RunID:       fmt.Sprintf("run_%d", time.Now().UnixNano()),
		SessionID:   forkSessionID,
		GraphID:     r.graph.ID,
		CurrentNode: cp.NodeID,
		Status:      RunStatusRunning,
		State:       forkedState,
		SeqNum:      cp.SeqNum,
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	return r.execute(ctx, rs, true)
}

// ReplayFrom loads a checkpoint and re-executes the graph from the checkpoint's node.
// All nodes before the checkpoint node are skipped (treated as cached).
// Re-execution starts at the checkpoint's node and continues to END.
func (r *Runner) ReplayFrom(ctx context.Context, checkpointID string) (*RunState, error) {
	cp, err := r.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("replay: checkpoint not found: %w", err)
	}

	rs := &RunState{
		RunID:       fmt.Sprintf("replay_%d", time.Now().UnixNano()),
		SessionID:   cp.SessionID,
		GraphID:     r.graph.ID,
		CurrentNode: cp.NodeID,
		Status:      RunStatusRunning,
		State:       State(cp.State),
		SeqNum:      cp.SeqNum,
		StartedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	r.emit(StreamEvent{Type: "replay_start", NodeID: cp.NodeID, State: rs.State})
	return r.execute(ctx, rs, true)
}

// execute drives the run loop. skipFirstInterrupt, when true, causes the first
// interrupt node encountered to execute instead of pausing — this is how a
// resumed (approved) workflow advances past its pause point exactly once.
func (r *Runner) execute(ctx context.Context, rs *RunState, skipFirstInterrupt bool) (*RunState, error) {
	if err := r.begin(); err != nil {
		return nil, err
	}
	defer r.closeLocalCh()

	// Start a top-level graph execution span
	var graphSpan *storage.Trace
	if r.tracer != nil {
		var spanErr error
		graphSpan, spanErr = r.tracer.StartSpan(ctx, rs.SessionID, "graph:"+r.graph.ID, "graph")
		if spanErr != nil {
			graphSpan = nil
		}
	}

	steps := 0
	for rs.Status == RunStatusRunning {
		// Terminal marker: the checkpoint of a completed run records EndNode as
		// the "next" node, so resuming a finished run completes immediately.
		if rs.CurrentNode == EndNode || rs.CurrentNode == "" {
			rs.Status = RunStatusCompleted
			r.emit(StreamEvent{Type: "completed", State: rs.State})
			break
		}

		// Step / cycle guard (P0-006).
		steps++
		if steps > r.maxSteps {
			rs.Status = RunStatusFailed
			err := fmt.Errorf("run exceeded max steps (%d): possible cycle in graph %q", r.maxSteps, r.graph.ID)
			r.emit(StreamEvent{Type: "error", NodeID: rs.CurrentNode, Error: err.Error()})
			if graphSpan != nil {
				_ = r.tracer.EndSpan(ctx, graphSpan, nil, err.Error())
			}
			return rs, err
		}

		node, ok := r.graph.Nodes[rs.CurrentNode]
		if !ok {
			rs.Status = RunStatusFailed
			if graphSpan != nil {
				_ = r.tracer.EndSpan(ctx, graphSpan, nil, fmt.Sprintf("node %q not found", rs.CurrentNode))
			}
			return rs, fmt.Errorf("node %q not found", rs.CurrentNode)
		}

		// Check for interrupt (human-in-the-loop pause). On resume we skip the
		// first interrupt exactly once so the approved node runs (P0-001).
		if node.Interrupt {
			if skipFirstInterrupt {
				skipFirstInterrupt = false
			} else {
				rs.Status = RunStatusPaused
				r.emit(StreamEvent{Type: "interrupt", NodeID: node.ID, State: rs.State})
				if r.store != nil {
					if err := r.commit(ctx, rs, node.ID, nil); err != nil {
						return rs, fmt.Errorf("checkpoint on interrupt: %w", err)
					}
				}
				if graphSpan != nil {
					_ = r.tracer.EndSpan(ctx, graphSpan, rs.State, "paused at interrupt node "+node.ID)
				}
				return rs, nil
			}
		}

		// Start node-level trace span
		var nodeSpan *storage.Trace
		if r.tracer != nil {
			var spanErr error
			nodeSpan, spanErr = r.tracer.StartSpan(ctx, rs.SessionID, "node:"+node.ID, "node")
			if spanErr != nil {
				nodeSpan = nil
			}
		}

		// Execute node with emitter context for custom events
		nodeCtx := ctx
		var cancel context.CancelFunc
		if r.nodeTimeout > 0 {
			nodeCtx, cancel = context.WithTimeout(nodeCtx, r.nodeTimeout)
		}
		var emitCh chan stream.Event
		if r.broker != nil {
			emitCh = make(chan stream.Event, 64)
			nodeCtx = stream.WithEmitter(nodeCtx, emitCh)
			go func() {
				for evt := range emitCh {
					r.broker.Publish(evt)
				}
			}()
		}
		r.emit(StreamEvent{Type: "node_start", NodeID: node.ID, State: rs.State})
		newState, err := r.callNode(nodeCtx, node, rs.State)
		if emitCh != nil {
			close(emitCh)
		}
		if cancel != nil {
			cancel()
		}
		if err != nil {
			rs.Status = RunStatusFailed
			r.emit(StreamEvent{Type: "error", NodeID: node.ID, Error: err.Error()})
			if nodeSpan != nil {
				_ = r.tracer.EndSpan(ctx, nodeSpan, nil, err.Error())
			}
			if graphSpan != nil {
				_ = r.tracer.EndSpan(ctx, graphSpan, nil, fmt.Sprintf("node %q failed: %s", node.ID, err.Error()))
			}
			return rs, fmt.Errorf("node %q: %w", node.ID, err)
		}
		rs.State = newState
		rs.SeqNum++
		rs.UpdatedAt = time.Now()
		r.emit(StreamEvent{Type: "node_end", NodeID: node.ID, State: rs.State})

		if nodeSpan != nil {
			_ = r.tracer.EndSpan(ctx, nodeSpan, rs.State, "")
		}

		// Determine the next node BEFORE checkpointing so the checkpoint records
		// the next node to execute, not the one that just finished (P0-002).
		next := r.findNext(rs.CurrentNode, rs.State)
		if next == EndNode || next == "" {
			rs.Status = RunStatusCompleted
			rs.CurrentNode = EndNode
		} else {
			rs.CurrentNode = next
		}

		// Checkpoint + ledger event after advancing (skip if no storage configured).
		if r.store != nil {
			evt := &storage.Event{
				ID:        fmt.Sprintf("evt_%s_%d", rs.RunID, rs.SeqNum),
				SessionID: rs.SessionID,
				SeqNum:    rs.SeqNum,
				Type:      "node_executed",
				Payload:   map[string]any{"node": node.ID, "state": rs.State},
				CreatedAt: time.Now(),
			}
			if err := r.commit(ctx, rs, rs.CurrentNode, evt); err != nil {
				rs.Status = RunStatusFailed
				return rs, fmt.Errorf("checkpoint: %w", err)
			}
		}

		if rs.Status == RunStatusCompleted {
			r.emit(StreamEvent{Type: "completed", State: rs.State})
		} else {
			r.emit(StreamEvent{Type: "edge_transition", NodeID: rs.CurrentNode})
		}
	}

	if graphSpan != nil {
		_ = r.tracer.EndSpan(ctx, graphSpan, rs.State, "")
	}

	return rs, nil
}

// callNode invokes a node function with panic recovery (P0-009). A panicking
// node fails only its own run instead of crashing the process.
func (r *Runner) callNode(ctx context.Context, node *Node, state State) (newState State, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			newState = nil
			err = fmt.Errorf("node %q panicked: %v", node.ID, rec)
		}
	}()
	return node.Fn(ctx, state)
}

func (r *Runner) findNext(from string, state State) string {
	edges := r.graph.AdjList[from]
	if len(edges) == 0 {
		return ""
	}
	e := edges[0]
	if e.Condition != nil {
		return e.Condition(state)
	}
	return e.To
}

// commit persists a checkpoint (recording nodeID as the next node to execute)
// and, when provided, its ledger event. It uses an atomic CheckpointCommitter
// when the store supports one, otherwise falls back to two idempotent calls and
// no longer discards the AppendEvent error (P0-004).
func (r *Runner) commit(ctx context.Context, rs *RunState, nodeID string, evt *storage.Event) error {
	cp := &storage.Checkpoint{
		ID:        fmt.Sprintf("cp_%s_%d", rs.RunID, rs.SeqNum),
		SessionID: rs.SessionID,
		RunID:     rs.RunID,
		NodeID:    nodeID,
		State:     rs.State,
		SeqNum:    rs.SeqNum,
		CreatedAt: time.Now(),
	}

	if cc, ok := r.store.(CheckpointCommitter); ok {
		if err := cc.SaveCheckpointAndEvent(ctx, cp, evt); err != nil {
			return fmt.Errorf("save checkpoint and event: %w", err)
		}
		return nil
	}

	if err := r.store.SaveCheckpoint(ctx, cp); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	if evt != nil {
		if err := r.store.AppendEvent(ctx, evt); err != nil {
			return fmt.Errorf("append event: %w", err)
		}
	}
	return nil
}
