package storage

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
)

// Pagination bounds. Adapters clamp caller-supplied limits into this range so a
// single request can never scan an unbounded ledger.
const (
	// DefaultPageLimit is used when a caller passes a non-positive limit.
	DefaultPageLimit = 100
	// MaxPageLimit caps how many rows a single paged read may return.
	MaxPageLimit = 1000
)

// ClampLimit normalizes a caller-supplied page limit into [1, MaxPageLimit],
// defaulting to DefaultPageLimit when limit <= 0.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return limit
}

// EventPage is a cursor-paginated slice of events. NextCursor is empty when the
// page is the last one.
type EventPage struct {
	Events     []*Event `json:"events"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// TracePage is a cursor-paginated slice of traces.
type TracePage struct {
	Traces     []*Trace `json:"traces"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// CheckpointPage is a cursor-paginated slice of checkpoints.
type CheckpointPage struct {
	Checkpoints []*Checkpoint `json:"checkpoints"`
	NextCursor  string        `json:"next_cursor,omitempty"`
}

// Paginator is an OPTIONAL interface a Storage adapter may implement to expose
// cursor-paginated reads of the unbounded ledgers (traces, events, checkpoints).
//
// It is deliberately separate from Storage so that adding it never breaks
// existing Storage implementations. Callers should type-assert:
//
//	if p, ok := store.(storage.Paginator); ok {
//		page, err := p.ListEventsPaged(ctx, sid, 0, 100, "")
//	}
//
// Pass an empty cursor to fetch the first page; pass the previous page's
// NextCursor to fetch the following page. Limits are clamped via ClampLimit.
type Paginator interface {
	ListEventsPaged(ctx context.Context, sessionID string, afterSeq int64, limit int, cursor string) (*EventPage, error)
	ListTracesPaged(ctx context.Context, sessionID string, limit int, cursor string) (*TracePage, error)
	ListCheckpointsPaged(ctx context.Context, sessionID string, limit int, cursor string) (*CheckpointPage, error)
}

// EncodeSeqCursor encodes a monotonic sequence number into an opaque cursor.
func EncodeSeqCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

// DecodeSeqCursor decodes a cursor produced by EncodeSeqCursor. An empty cursor
// decodes to 0 with no error.
func DecodeSeqCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("decode cursor: %w", err)
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cursor: %w", err)
	}
	return n, nil
}

// EncodeStrCursor encodes a string key (e.g. a row's primary-key id) into an
// opaque cursor for keyset pagination.
func EncodeStrCursor(key string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(key))
}

// DecodeStrCursor decodes a cursor produced by EncodeStrCursor. An empty cursor
// decodes to the empty string with no error.
func DecodeStrCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", fmt.Errorf("decode cursor: %w", err)
	}
	return string(b), nil
}
