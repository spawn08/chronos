// Package trace provides execution tracing and audit logging.
package trace

import (
	"context"
	"fmt"
	"time"

	"github.com/chronos-ai/chronos/storage"
)

// Collector records traces and audit events.
type Collector struct {
	store storage.Storage
}

func NewCollector(store storage.Storage) *Collector {
	return &Collector{store: store}
}

// StartSpan begins a new trace span.
func (c *Collector) StartSpan(ctx context.Context, sessionID, name, kind string) (*storage.Trace, error) {
	t := &storage.Trace{
		ID:        fmt.Sprintf("trace_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Name:      name,
		Kind:      kind,
		StartedAt: time.Now(),
	}
	return t, c.store.InsertTrace(ctx, t)
}

// EndSpan completes a trace span.
func (c *Collector) EndSpan(ctx context.Context, t *storage.Trace, output any, errMsg string) error {
	t.Output = output
	t.Error = errMsg
	t.EndedAt = time.Now()
	// Re-insert (upsert) — in production, use UPDATE
	return c.store.InsertTrace(ctx, t)
}

// Audit records a security audit event.
func (c *Collector) Audit(ctx context.Context, sessionID, actor, action, resource string) error {
	return c.store.AppendAuditLog(ctx, &storage.AuditLog{
		ID:        fmt.Sprintf("audit_%d", time.Now().UnixNano()),
		SessionID: sessionID,
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		CreatedAt: time.Now(),
	})
}
