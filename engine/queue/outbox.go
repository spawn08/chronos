package queue

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Dispatcher delivers an outbox entry to its external destination. It must be
// safe to call more than once for the same entry (delivery may be retried after
// a crash between send and the sent-mark); use the entry's IdempotencyKey to
// deduplicate downstream if the destination is not naturally idempotent.
type Dispatcher interface {
	Dispatch(ctx context.Context, e *OutboxEntry) error
}

// DispatcherFunc adapts a function to the Dispatcher interface.
type DispatcherFunc func(ctx context.Context, e *OutboxEntry) error

// Dispatch calls f.
func (f DispatcherFunc) Dispatch(ctx context.Context, e *OutboxEntry) error { return f(ctx, e) }

// DefaultOutboxMaxAttempts is the number of delivery attempts after which a
// permanently-failing outbox entry is dead-lettered (marked OutboxFailed) so it
// stops being claimed and can no longer starve newer effects.
const DefaultOutboxMaxAttempts = 10

// Outbox records external effects transactionally with run progress and drains
// them reliably. Recording an effect twice with the same idempotency key is a
// no-op, so retries and resumes never double-emit (P1-003).
type Outbox struct {
	store       Store
	now         func() time.Time
	maxAttempts int
}

// NewOutbox constructs an Outbox over a Store with the default dead-letter cap.
func NewOutbox(store Store) *Outbox {
	return &Outbox{store: store, now: time.Now, maxAttempts: DefaultOutboxMaxAttempts}
}

// WithMaxAttempts sets the delivery-attempt cap after which an entry is
// dead-lettered. A value <= 0 restores the default. It returns the Outbox for
// chaining.
func (o *Outbox) WithMaxAttempts(n int) *Outbox {
	if n <= 0 {
		n = DefaultOutboxMaxAttempts
	}
	o.maxAttempts = n
	return o
}

// Record persists an external effect for later delivery. The idempotencyKey must
// be stable for a given logical effect so retries collapse to one row.
func (o *Outbox) Record(ctx context.Context, sessionID, idempotencyKey, topic string, payload []byte) error {
	if idempotencyKey == "" {
		return fmt.Errorf("outbox record: empty idempotency key")
	}
	e := &OutboxEntry{
		SessionID:      sessionID,
		IdempotencyKey: idempotencyKey,
		Topic:          topic,
		Payload:        payload,
		Status:         OutboxPending,
		CreatedAt:      o.now(),
	}
	if err := o.store.EnqueueOutbox(ctx, e); err != nil {
		return fmt.Errorf("outbox record: %w", err)
	}
	return nil
}

// DrainOnce dispatches up to limit pending entries. It returns the number of
// entries successfully delivered. Failed dispatches remain pending for retry.
func (o *Outbox) DrainOnce(ctx context.Context, d Dispatcher, limit int) (int, error) {
	entries, err := o.store.ClaimOutbox(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("outbox drain: claim: %w", err)
	}
	sent := 0
	for _, e := range entries {
		if err := d.Dispatch(ctx, e); err != nil {
			if markErr := o.store.MarkOutboxFailed(ctx, e.ID, err.Error(), o.maxAttempts, o.now()); markErr != nil {
				return sent, fmt.Errorf("outbox drain: mark failed: %w", markErr)
			}
			continue
		}
		if err := o.store.MarkOutboxSent(ctx, e.ID, o.now()); err != nil {
			return sent, fmt.Errorf("outbox drain: mark sent: %w", err)
		}
		sent++
	}
	return sent, nil
}

// Drain runs DrainOnce on a ticker until ctx is canceled.
func (o *Outbox) Drain(ctx context.Context, d Dispatcher, interval time.Duration, batch int) error {
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if _, err := o.DrainOnce(ctx, d, batch); err != nil {
				if errors.Is(err, context.Canceled) {
					return nil
				}
				continue
			}
		}
	}
}
