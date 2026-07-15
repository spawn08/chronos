package storage

import (
	"context"
	"time"
)

// Retention is an OPTIONAL interface a Storage adapter may implement to trim the
// unbounded, append-only tables (events, traces, audit logs, checkpoints). It is
// separate from Storage so that adding it never breaks existing implementations.
//
// Each Trim* method returns the number of rows removed. Callers should
// type-assert:
//
//	if r, ok := store.(storage.Retention); ok {
//		_, _ = r.TrimEvents(ctx, time.Now().Add(-30*24*time.Hour))
//	}
type Retention interface {
	// TrimEvents deletes events created strictly before olderThan.
	TrimEvents(ctx context.Context, olderThan time.Time) (int64, error)
	// TrimTraces deletes traces started strictly before olderThan.
	TrimTraces(ctx context.Context, olderThan time.Time) (int64, error)
	// TrimAuditLogs deletes audit logs created strictly before olderThan.
	TrimAuditLogs(ctx context.Context, olderThan time.Time) (int64, error)
	// TrimCheckpoints keeps only the most recent keep checkpoints (by seq_num)
	// for the given session and deletes the rest. keep <= 0 is a no-op.
	TrimCheckpoints(ctx context.Context, sessionID string, keep int) (int64, error)
}

// RetentionPolicy describes a time-and-count based retention window that a
// caller (e.g. a background job) can apply via ApplyRetention.
type RetentionPolicy struct {
	// MaxAge deletes events, traces and audit logs older than now-MaxAge.
	// Zero disables age-based trimming.
	MaxAge time.Duration `json:"max_age,omitempty"`
	// KeepCheckpointsPerSession limits checkpoints retained per session.
	// Zero disables checkpoint trimming.
	KeepCheckpointsPerSession int `json:"keep_checkpoints_per_session,omitempty"`
}

// RetentionResult reports how many rows each Trim* step removed.
type RetentionResult struct {
	Events      int64 `json:"events"`
	Traces      int64 `json:"traces"`
	AuditLogs   int64 `json:"audit_logs"`
	Checkpoints int64 `json:"checkpoints"`
}

// ApplyRetention runs a RetentionPolicy against a Retention store. The
// sessionIDs argument scopes checkpoint trimming (checkpoint retention is
// per-session); pass nil to skip checkpoint trimming.
func ApplyRetention(ctx context.Context, r Retention, policy RetentionPolicy, sessionIDs []string) (RetentionResult, error) {
	var res RetentionResult
	if policy.MaxAge > 0 {
		cutoff := time.Now().Add(-policy.MaxAge)
		n, err := r.TrimEvents(ctx, cutoff)
		if err != nil {
			return res, err
		}
		res.Events = n
		if n, err = r.TrimTraces(ctx, cutoff); err != nil {
			return res, err
		}
		res.Traces = n
		if n, err = r.TrimAuditLogs(ctx, cutoff); err != nil {
			return res, err
		}
		res.AuditLogs = n
	}
	if policy.KeepCheckpointsPerSession > 0 {
		for _, sid := range sessionIDs {
			n, err := r.TrimCheckpoints(ctx, sid, policy.KeepCheckpointsPerSession)
			if err != nil {
				return res, err
			}
			res.Checkpoints += n
		}
	}
	return res, nil
}
