// Package graph provides the StateGraph durable execution runtime.
package graph

import (
	"context"
	"time"
)

// State is the mutable state bag passed through graph execution.
type State map[string]any

// NodeFunc is the function signature for a graph node.
// It receives the current state and returns updated state.
type NodeFunc func(ctx context.Context, state State) (State, error)

// EdgeCondition decides which node to transition to based on state.
// Returns the target node ID.
type EdgeCondition func(state State) string

// Node represents a node in the state graph.
type Node struct {
	ID string
	Fn NodeFunc
	// Interrupt, if true, causes the runner to checkpoint and pause before executing this node.
	Interrupt bool
}

// Edge represents a transition between nodes.
type Edge struct {
	From      string
	To        string        // static target (mutually exclusive with Condition)
	Condition EdgeCondition // dynamic routing (mutually exclusive with To)
}

// RunStatus represents the status of a graph run.
type RunStatus string

const (
	RunStatusRunning   RunStatus = "running"
	RunStatusPaused    RunStatus = "paused"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// RunState captures the full runtime state for a single graph execution.
type RunState struct {
	RunID       string    `json:"run_id"`
	SessionID   string    `json:"session_id"`
	GraphID     string    `json:"graph_id"`
	CurrentNode string    `json:"current_node"`
	Status      RunStatus `json:"status"`
	State       State     `json:"state"`
	SeqNum      int64     `json:"seq_num"`
	StartedAt   time.Time `json:"started_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Rich output (Agno-inspired)
	Messages   []Message        `json:"messages,omitempty"`
	ToolCalls  []ToolCallRecord `json:"tool_calls,omitempty"`
	TotalUsage UsageStats       `json:"total_usage"`
}

// Message records a message exchanged during the run.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ToolCallRecord records a tool invocation during the run.
type ToolCallRecord struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args"`
	Result any            `json:"result"`
	Error  string         `json:"error,omitempty"`
}

// UsageStats tracks cumulative token usage across a run.
type UsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamEvent is emitted during graph execution for real-time observability.
type StreamEvent struct {
	Type      string    `json:"type"` // node_start, node_end, edge_transition, checkpoint, interrupt, error
	NodeID    string    `json:"node_id,omitempty"`
	State     State     `json:"state,omitempty"`
	Error     string    `json:"error,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
