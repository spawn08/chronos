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
		_ = json.Unmarshal([]byte(payload), &e.Payload)
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
		_ = json.Unmarshal([]byte(state), &cp.State)
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
		_ = json.Unmarshal([]byte(inp), &t.Input)
		_ = json.Unmarshal([]byte(outp), &t.Output)
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

// TrimEvents deletes events created strictly before olderThan.
func (s *Store) TrimEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < ?`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim events: %w", err)
	}
	return res.RowsAffected()
}

// TrimTraces deletes traces started strictly before olderThan.
func (s *Store) TrimTraces(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE started_at < ?`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim traces: %w", err)
	}
	return res.RowsAffected()
}

// TrimAuditLogs deletes audit logs created strictly before olderThan.
func (s *Store) TrimAuditLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < ?`, olderThan)
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

// AppendEvents appends many events in a single multi-row INSERT within one
// transaction. It is idempotent on event id (INSERT OR IGNORE).
func (s *Store) AppendEvents(ctx context.Context, events []*storage.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var b strings.Builder
	b.WriteString(`INSERT OR IGNORE INTO events (id, session_id, seq_num, type, payload, created_at) VALUES `)
	args := make([]any, 0, len(events)*6)
	for i, e := range events {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`(?,?,?,?,?,?)`)
		payload, _ := json.Marshal(e.Payload)
		args = append(args, e.ID, e.SessionID, e.SeqNum, e.Type, string(payload), e.CreatedAt)
	}
	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("batch append events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch events: %w", err)
	}
	return nil
}

// InsertTraces inserts many trace spans in a single multi-row INSERT within one
// transaction.
func (s *Store) InsertTraces(ctx context.Context, traces []*storage.Trace) error {
	if len(traces) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var b strings.Builder
	b.WriteString(`INSERT OR REPLACE INTO traces (id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at) VALUES `)
	args := make([]any, 0, len(traces)*10)
	for i, t := range traces {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`(?,?,?,?,?,?,?,?,?,?)`)
		inp, _ := json.Marshal(t.Input)
		outp, _ := json.Marshal(t.Output)
		args = append(args, t.ID, t.SessionID, t.ParentID, t.Name, t.Kind, string(inp), string(outp), t.Error, t.StartedAt, t.EndedAt)
	}
	if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("batch insert traces: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch traces: %w", err)
	}
	return nil
}
