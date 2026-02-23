// Package storage defines the core persistence interfaces for Chronos.
package storage

import (
	"context"
	"time"
)

// Session represents an agent execution session.
type Session struct {
	ID        string            `json:"id"`
	AgentID   string            `json:"agent_id"`
	Status    string            `json:"status"` // running, paused, completed, failed
	Metadata  map[string]any    `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// MemoryRecord represents a short-term or long-term memory entry.
type MemoryRecord struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id,omitempty"` // empty = long-term
	AgentID   string         `json:"agent_id"`
	UserID    string         `json:"user_id,omitempty"`
	Kind      string         `json:"kind"` // short_term, long_term
	Key       string         `json:"key"`
	Value     any            `json:"value"`
	CreatedAt time.Time      `json:"created_at"`
}

// AuditLog records a security-relevant event.
type AuditLog struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	Resource  string         `json:"resource"`
	Detail    map[string]any `json:"detail,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// Trace represents a single trace span for observability.
type Trace struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"` // node, tool_call, model_call, approval
	Input     any            `json:"input,omitempty"`
	Output    any            `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	StartedAt time.Time      `json:"started_at"`
	EndedAt   time.Time      `json:"ended_at,omitempty"`
}

// Event is an append-only ledger entry for replayability.
type Event struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	SeqNum    int64          `json:"seq_num"`
	Type      string         `json:"type"`
	Payload   any            `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// Checkpoint captures the full state of a run for resume/time-travel.
type Checkpoint struct {
	ID        string         `json:"id"`
	SessionID string         `json:"session_id"`
	RunID     string         `json:"run_id"`
	NodeID    string         `json:"node_id"`
	State     map[string]any `json:"state"`
	SeqNum    int64          `json:"seq_num"`
	CreatedAt time.Time      `json:"created_at"`
}

// Storage is the primary persistence interface. All adapters must implement this.
type Storage interface {
	// Sessions
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	UpdateSession(ctx context.Context, s *Session) error
	ListSessions(ctx context.Context, agentID string, limit, offset int) ([]*Session, error)

	// Memory
	PutMemory(ctx context.Context, m *MemoryRecord) error
	GetMemory(ctx context.Context, agentID, key string) (*MemoryRecord, error)
	ListMemory(ctx context.Context, agentID string, kind string) ([]*MemoryRecord, error)
	DeleteMemory(ctx context.Context, id string) error

	// Audit logs
	AppendAuditLog(ctx context.Context, log *AuditLog) error
	ListAuditLogs(ctx context.Context, sessionID string, limit, offset int) ([]*AuditLog, error)

	// Traces
	InsertTrace(ctx context.Context, t *Trace) error
	GetTrace(ctx context.Context, id string) (*Trace, error)
	ListTraces(ctx context.Context, sessionID string) ([]*Trace, error)

	// Event ledger (append-only)
	AppendEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, sessionID string, afterSeq int64) ([]*Event, error)

	// Checkpoints
	SaveCheckpoint(ctx context.Context, cp *Checkpoint) error
	GetCheckpoint(ctx context.Context, id string) (*Checkpoint, error)
	GetLatestCheckpoint(ctx context.Context, sessionID string) (*Checkpoint, error)
	ListCheckpoints(ctx context.Context, sessionID string) ([]*Checkpoint, error)

	// Lifecycle
	Migrate(ctx context.Context) error
	Close() error
}
