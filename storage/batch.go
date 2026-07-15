package storage

import "context"

// BatchIngester is an OPTIONAL interface a Storage adapter may implement to
// ingest many rows in a single round-trip (multi-row INSERT / COPY) instead of
// one statement per row. It is separate from Storage so that adding it never
// breaks existing implementations. Callers should type-assert:
//
//	if b, ok := store.(storage.BatchIngester); ok {
//		err := b.AppendEvents(ctx, events)
//	} else {
//		for _, e := range events { _ = store.AppendEvent(ctx, e) }
//	}
type BatchIngester interface {
	// AppendEvents appends many ledger events atomically in one batch. Like
	// AppendEvent it is idempotent on the event id.
	AppendEvents(ctx context.Context, events []*Event) error
	// InsertTraces inserts many trace spans in one batch.
	InsertTraces(ctx context.Context, traces []*Trace) error
}
