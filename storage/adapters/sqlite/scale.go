package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Compile-time checks that Store implements the optional scale interfaces.
var (
	_ storage.Paginator     = (*Store)(nil)
	_ storage.Retention     = (*Store)(nil)
	_ storage.BatchIngester = (*Store)(nil)
)

// sqliteMaxBindParams is a conservative cap on the number of bind parameters in a
// single statement. SQLite's historical limit (SQLITE_MAX_VARIABLE_NUMBER) is 999
// on older builds and 32766 on newer ones; staying under 999 keeps multi-row
// inserts safe on every build. Batches whose total parameter count would exceed
// this are split into several statements within a single transaction, preserving
// both atomicity and the ON CONFLICT/OR IGNORE idempotency semantics.
const sqliteMaxBindParams = 900

// --- Pagination (P1-012) ---

// ListEventsPaged returns a cursor-paginated page of events after afterSeq,
// ordered by seq_num. It fetches limit+1 rows to detect whether a further page
// exists, so the final page never carries a dangling cursor.
func (s *Store) ListEventsPaged(ctx context.Context, sessionID string, afterSeq int64, limit int, cursor string) (*storage.EventPage, error) {
	limit = storage.ClampLimit(limit)
	cur, err := storage.DecodeSeqCursor(cursor)
	if err != nil {
		return nil, err
	}
	if cur > afterSeq {
		afterSeq = cur
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq_num, type, payload, created_at FROM events WHERE session_id=? AND seq_num>? ORDER BY seq_num LIMIT ?`,
		sessionID, afterSeq, limit+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &storage.EventPage{}
	for rows.Next() {
		e := &storage.Event{}
		var payload string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.SeqNum, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
			return nil, fmt.Errorf("unmarshal event payload: %w", err)
		}
		page.Events = append(page.Events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Events) > limit {
		page.Events = page.Events[:limit]
		page.NextCursor = storage.EncodeSeqCursor(page.Events[limit-1].SeqNum)
	}
	return page, nil
}

// ListCheckpointsPaged returns a cursor-paginated page of checkpoints ordered by
// seq_num.
func (s *Store) ListCheckpointsPaged(ctx context.Context, sessionID string, limit int, cursor string) (*storage.CheckpointPage, error) {
	limit = storage.ClampLimit(limit)
	after, err := storage.DecodeSeqCursor(cursor)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE session_id=? AND seq_num>? ORDER BY seq_num LIMIT ?`,
		sessionID, after, limit+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &storage.CheckpointPage{}
	for rows.Next() {
		cp := &storage.Checkpoint{}
		var state string
		if err := rows.Scan(&cp.ID, &cp.SessionID, &cp.RunID, &cp.NodeID, &state, &cp.SeqNum, &cp.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(state), &cp.State); err != nil {
			return nil, fmt.Errorf("unmarshal checkpoint state: %w", err)
		}
		page.Checkpoints = append(page.Checkpoints, cp)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Checkpoints) > limit {
		page.Checkpoints = page.Checkpoints[:limit]
		page.NextCursor = storage.EncodeSeqCursor(page.Checkpoints[limit-1].SeqNum)
	}
	return page, nil
}

// ListTracesPaged returns a cursor-paginated page of traces. It keysets on the
// stable primary key id (rather than the wall-clock started_at, whose text
// storage makes cross-timezone comparison unreliable), which gives a
// deterministic order across SQLite and Postgres.
func (s *Store) ListTracesPaged(ctx context.Context, sessionID string, limit int, cursor string) (*storage.TracePage, error) {
	limit = storage.ClampLimit(limit)
	afterID, err := storage.DecodeStrCursor(cursor)
	if err != nil {
		return nil, err
	}

	const cols = `id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at`
	var r *sql.Rows
	if afterID == "" {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM traces WHERE session_id=? ORDER BY id LIMIT ?`,
			sessionID, limit+1,
		)
	} else {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM traces WHERE session_id=? AND id>? ORDER BY id LIMIT ?`,
			sessionID, afterID, limit+1,
		)
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	page := &storage.TracePage{}
	for r.Next() {
		t := &storage.Trace{}
		var inp, outp string
		if err := r.Scan(&t.ID, &t.SessionID, &t.ParentID, &t.Name, &t.Kind, &inp, &outp, &t.Error, &t.StartedAt, &t.EndedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(inp), &t.Input); err != nil {
			return nil, fmt.Errorf("unmarshal trace input: %w", err)
		}
		if err := json.Unmarshal([]byte(outp), &t.Output); err != nil {
			return nil, fmt.Errorf("unmarshal trace output: %w", err)
		}
		page.Traces = append(page.Traces, t)
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	if len(page.Traces) > limit {
		page.Traces = page.Traces[:limit]
		page.NextCursor = storage.EncodeStrCursor(page.Traces[limit-1].ID)
	}
	return page, nil
}

// --- Retention (P1-012) ---
//
// Age-based trimming compares instants, not raw text. The go-sqlite3 driver
// stores time.Time as an RFC3339 string that carries the value's timezone offset
// (e.g. "…Z", "…+05:30", "…-08:00"), so a plain lexical `created_at < ?` compares
// the wall-clock text and mis-orders rows written in different timezones — the
// same reason ListTracesPaged keysets on the stable id PK instead of started_at.
// Wrapping both sides in julianday() makes SQLite parse the offset and compare
// true UTC instants, which is correct regardless of the stored timezone. (The
// Postgres adapter needs no such wrapper because its columns are TIMESTAMPTZ,
// which already compares as instants.)

// TrimEvents deletes events created strictly before olderThan.
func (s *Store) TrimEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE julianday(created_at) < julianday(?)`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim events: %w", err)
	}
	return res.RowsAffected()
}

// TrimTraces deletes traces started strictly before olderThan.
func (s *Store) TrimTraces(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE julianday(started_at) < julianday(?)`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim traces: %w", err)
	}
	return res.RowsAffected()
}

// TrimAuditLogs deletes audit logs created strictly before olderThan.
func (s *Store) TrimAuditLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE julianday(created_at) < julianday(?)`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim audit logs: %w", err)
	}
	return res.RowsAffected()
}

// TrimCheckpoints keeps only the most recent keep checkpoints (by seq_num) for
// the session and deletes the rest.
func (s *Store) TrimCheckpoints(ctx context.Context, sessionID string, keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM checkpoints WHERE session_id=? AND seq_num NOT IN (
			SELECT seq_num FROM checkpoints WHERE session_id=? ORDER BY seq_num DESC LIMIT ?
		)`,
		sessionID, sessionID, keep,
	)
	if err != nil {
		return 0, fmt.Errorf("trim checkpoints: %w", err)
	}
	return res.RowsAffected()
}

// --- Batch ingestion (P1-013) ---

// AppendEvents appends many events within one transaction. It is idempotent on
// event id (INSERT OR IGNORE). The batch is split into chunks so the per-statement
// bind-parameter count stays under sqliteMaxBindParams; all chunks run in the same
// transaction, so the whole batch still commits (or rolls back) atomically.
func (s *Store) AppendEvents(ctx context.Context, events []*storage.Event) error {
	if len(events) == 0 {
		return nil
	}
	const paramsPerRow = 6
	rowsPerChunk := sqliteMaxBindParams / paramsPerRow

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for start := 0; start < len(events); start += rowsPerChunk {
		end := start + rowsPerChunk
		if end > len(events) {
			end = len(events)
		}
		chunk := events[start:end]

		var b strings.Builder
		b.WriteString(`INSERT OR IGNORE INTO events (id, session_id, seq_num, type, payload, created_at) VALUES `)
		args := make([]any, 0, len(chunk)*paramsPerRow)
		for i, e := range chunk {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`(?,?,?,?,?,?)`)
			payload, err := json.Marshal(e.Payload)
			if err != nil {
				return fmt.Errorf("marshal event payload: %w", err)
			}
			args = append(args, e.ID, e.SessionID, e.SeqNum, e.Type, string(payload), e.CreatedAt)
		}
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("batch append events: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch events: %w", err)
	}
	return nil
}

// InsertTraces inserts many trace spans within one transaction. As with
// AppendEvents the batch is chunked to respect sqliteMaxBindParams while keeping
// the whole insert atomic.
func (s *Store) InsertTraces(ctx context.Context, traces []*storage.Trace) error {
	if len(traces) == 0 {
		return nil
	}
	const paramsPerRow = 10
	rowsPerChunk := sqliteMaxBindParams / paramsPerRow

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for start := 0; start < len(traces); start += rowsPerChunk {
		end := start + rowsPerChunk
		if end > len(traces) {
			end = len(traces)
		}
		chunk := traces[start:end]

		var b strings.Builder
		b.WriteString(`INSERT OR REPLACE INTO traces (id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at) VALUES `)
		args := make([]any, 0, len(chunk)*paramsPerRow)
		for i, t := range chunk {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`(?,?,?,?,?,?,?,?,?,?)`)
			inp, err := json.Marshal(t.Input)
			if err != nil {
				return fmt.Errorf("marshal trace input: %w", err)
			}
			outp, err := json.Marshal(t.Output)
			if err != nil {
				return fmt.Errorf("marshal trace output: %w", err)
			}
			args = append(args, t.ID, t.SessionID, t.ParentID, t.Name, t.Kind, string(inp), string(outp), t.Error, t.StartedAt, t.EndedAt)
		}
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("batch insert traces: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch traces: %w", err)
	}
	return nil
}
