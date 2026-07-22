package graph

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/queue"
	"github.com/spawn08/chronos/storage"
)

// RunPayload is the durable envelope carried by a queued graph run. It is stored
// as the queue.Run payload so a worker on any node can reconstruct the execution
// without shared in-process state. When Resume is true the executor continues
// from the session's latest checkpoint instead of starting fresh; this is how a
// human-in-the-loop run, once parked and later signaled, advances past its
// interrupt rather than restarting.
type RunPayload struct {
	Initial State `json:"initial,omitempty"`
	Resume  bool  `json:"resume,omitempty"`
}

// GraphResolver returns the compiled graph for a graph ID. A distributed worker
// uses it to look up the graph definition it must execute for a claimed run.
type GraphResolver func(graphID string) (*CompiledGraph, error)

// SingleGraphResolver returns a resolver that always yields g.
func SingleGraphResolver(g *CompiledGraph) GraphResolver {
	return func(graphID string) (*CompiledGraph, error) {
		if graphID != "" && graphID != g.ID {
			return nil, fmt.Errorf("resolver: unknown graph %q", graphID)
		}
		return g, nil
	}
}

// QueuedExecutor bridges the durable work queue to the existing graph Runner. It
// implements the intake/execution split: producers enqueue runs and
// this executor — invoked by any queue.Worker — runs or resumes the graph via a
// freshly constructed Runner, exactly as a synchronous caller would. The public
// Runner API is unchanged; this only calls it.
type QueuedExecutor struct {
	store    storage.Storage
	resolve  GraphResolver
	maxSteps int
}

// NewQueuedExecutor constructs a QueuedExecutor. store is the durable checkpoint
// store the Runner uses; resolve maps a run's GraphID to its compiled graph.
func NewQueuedExecutor(store storage.Storage, resolve GraphResolver) *QueuedExecutor {
	return &QueuedExecutor{store: store, resolve: resolve}
}

// WithMaxSteps bounds node executions per attempt (see Runner.WithMaxSteps).
func (qe *QueuedExecutor) WithMaxSteps(n int) *QueuedExecutor {
	qe.maxSteps = n
	return qe
}

// ApprovalSignal is the signal name a parked human-in-the-loop run waits on. A
// webhook handler resumes the run by delivering this signal for the session.
func ApprovalSignal(sessionID string) string {
	return "approval:" + sessionID
}

// Executor returns a queue.Executor that runs/resumes the graph for a claimed
// run. Wire it into a queue.Worker: queue.NewWorker(q, qe.Executor(), cfg).
func (qe *QueuedExecutor) Executor() queue.Executor {
	return qe.execute
}

func (qe *QueuedExecutor) execute(ctx context.Context, r *queue.Run) queue.Result {
	g, err := qe.resolve(r.GraphID)
	if err != nil {
		return queue.Result{Err: fmt.Errorf("queued executor: resolve graph: %w", err)}
	}

	var payload RunPayload
	if len(r.Payload) > 0 {
		if uerr := json.Unmarshal(r.Payload, &payload); uerr != nil {
			return queue.Result{Err: fmt.Errorf("queued executor: decode payload: %w", uerr)}
		}
	}
	// The queue Kind is an intake hint; the durable payload is authoritative so a
	// run re-enqueued after a HITL pause resumes instead of restarting.
	resume := payload.Resume || r.Kind == queue.KindResume

	runner := NewRunner(g, qe.store)
	if qe.maxSteps > 0 {
		runner = runner.WithMaxSteps(qe.maxSteps)
	}

	var rs *RunState
	if resume {
		rs, err = runner.Resume(ctx, r.SessionID)
	} else {
		initial := payload.Initial
		if initial == nil {
			initial = State{}
		}
		rs, err = runner.Run(ctx, r.SessionID, initial)
	}
	if err != nil {
		return queue.Result{Err: fmt.Errorf("queued executor: execute graph %q: %w", g.ID, err)}
	}

	switch rs.Status {
	case RunStatusPaused:
		// Human-in-the-loop: park until the approval signal arrives, and mark the
		// run to resume on its next attempt.
		patch, mErr := json.Marshal(RunPayload{Resume: true})
		if mErr != nil {
			return queue.Result{Err: fmt.Errorf("queued executor: marshal resume payload: %w", mErr)}
		}
		return queue.Result{ParkSignal: ApprovalSignal(r.SessionID), Patch: patch}
	case RunStatusCompleted:
		return queue.Result{}
	default:
		return queue.Result{Err: fmt.Errorf("queued executor: graph %q ended in status %q", g.ID, rs.Status)}
	}
}
