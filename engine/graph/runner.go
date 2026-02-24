package graph

import (
	"context"
	"fmt"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Runner executes a CompiledGraph with durable checkpointing.
type Runner struct {
	graph   *CompiledGraph
	store   storage.Storage
	stream  chan StreamEvent
}

// NewRunner creates a runner for the given compiled graph.
func NewRunner(g *CompiledGraph, store storage.Storage) *Runner {
	return &Runner{
		graph:  g,
		store:  store,
		stream: make(chan StreamEvent, 256),
	}
}

// Stream returns a channel of execution events for real-time observability.
func (r *Runner) Stream() <-chan StreamEvent {
	return r.stream
}

func (r *Runner) emit(evt StreamEvent) {
	evt.Timestamp = time.Now()
	select {
	case r.stream <- evt:
	default:
	}
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

	return r.execute(ctx, rs)
}

// Resume continues execution from the latest checkpoint for the given session.
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

	return r.execute(ctx, rs)
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

	return r.execute(ctx, rs)
}

func (r *Runner) execute(ctx context.Context, rs *RunState) (*RunState, error) {
	for rs.Status == RunStatusRunning {
		node, ok := r.graph.Nodes[rs.CurrentNode]
		if !ok {
			rs.Status = RunStatusFailed
			return rs, fmt.Errorf("node %q not found", rs.CurrentNode)
		}

		// Check for interrupt (human-in-the-loop pause)
		if node.Interrupt {
			rs.Status = RunStatusPaused
			r.emit(StreamEvent{Type: "interrupt", NodeID: node.ID, State: rs.State})
			if err := r.checkpoint(ctx, rs); err != nil {
				return rs, fmt.Errorf("checkpoint on interrupt: %w", err)
			}
			return rs, nil
		}

		// Execute node
		r.emit(StreamEvent{Type: "node_start", NodeID: node.ID, State: rs.State})
		newState, err := node.Fn(ctx, rs.State)
		if err != nil {
			rs.Status = RunStatusFailed
			r.emit(StreamEvent{Type: "error", NodeID: node.ID, Error: err.Error()})
			return rs, fmt.Errorf("node %q: %w", node.ID, err)
		}
		rs.State = newState
		rs.SeqNum++
		rs.UpdatedAt = time.Now()
		r.emit(StreamEvent{Type: "node_end", NodeID: node.ID, State: rs.State})

		// Checkpoint after each node
		if err := r.checkpoint(ctx, rs); err != nil {
			return rs, fmt.Errorf("checkpoint: %w", err)
		}

		// Append event to ledger
		_ = r.store.AppendEvent(ctx, &storage.Event{
			ID:        fmt.Sprintf("evt_%s_%d", rs.RunID, rs.SeqNum),
			SessionID: rs.SessionID,
			SeqNum:    rs.SeqNum,
			Type:      "node_executed",
			Payload:   map[string]any{"node": node.ID, "state": rs.State},
			CreatedAt: time.Now(),
		})

		// Find next node
		next := r.findNext(rs.CurrentNode, rs.State)
		if next == EndNode || next == "" {
			rs.Status = RunStatusCompleted
			r.emit(StreamEvent{Type: "completed", State: rs.State})
		} else {
			r.emit(StreamEvent{Type: "edge_transition", NodeID: next})
			rs.CurrentNode = next
		}
	}

	defer close(r.stream)
	return rs, nil
}

func (r *Runner) findNext(from string, state State) string {
	edges := r.graph.AdjList[from]
	for _, e := range edges {
		if e.Condition != nil {
			return e.Condition(state)
		}
		return e.To
	}
	return ""
}

func (r *Runner) checkpoint(ctx context.Context, rs *RunState) error {
	cp := &storage.Checkpoint{
		ID:        fmt.Sprintf("cp_%s_%d", rs.RunID, rs.SeqNum),
		SessionID: rs.SessionID,
		RunID:     rs.RunID,
		NodeID:    rs.CurrentNode,
		State:     rs.State,
		SeqNum:    rs.SeqNum,
		CreatedAt: time.Now(),
	}
	return r.store.SaveCheckpoint(ctx, cp)
}
