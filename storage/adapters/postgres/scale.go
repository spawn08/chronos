package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/spawn08/chronos/storage"
)

// Compile-time checks that Store implements the optional scale interfaces.
var (
	_ storage.Paginator     = (*Store)(nil)
	_ storage.Retention     = (*Store)(nil)
	_ storage.BatchIngester = (*Store)(nil)
)

// pgMaxBindParams is a conservative cap on the number of bind parameters in a
// single statement. PostgreSQL's wire protocol limits a statement to 65535
// parameters; staying at 60000 leaves headroom. Batches whose total parameter
// count would exceed this are split into several statements within one
// transaction, preserving both atomicity and ON CONFLICT DO NOTHING idempotency.
// (InsertTraces uses the COPY protocol, which has no parameter limit, so it is
// not chunked.)
const pgMaxBindParams = 60000

// --- Pagination ---

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
	tenant := storage.TenantFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, session_id, seq_num, type, payload, created_at FROM events WHERE tenant_id=$1 AND session_id=$2 AND seq_num>$3 ORDER BY seq_num LIMIT $4`,
		tenant, sessionID, afterSeq, limit+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &storage.EventPage{}
	for rows.Next() {
		e := &storage.Event{}
		var payload []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &e.SessionID, &e.SeqNum, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &e.Payload); err != nil {
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
	tenant := storage.TenantFromContext(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, session_id, run_id, node_id, state, seq_num, created_at FROM checkpoints WHERE tenant_id=$1 AND session_id=$2 AND seq_num>$3 ORDER BY seq_num LIMIT $4`,
		tenant, sessionID, after, limit+1,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &storage.CheckpointPage{}
	for rows.Next() {
		cp := &storage.Checkpoint{}
		var state []byte
		if err := rows.Scan(&cp.ID, &cp.TenantID, &cp.SessionID, &cp.RunID, &cp.NodeID, &state, &cp.SeqNum, &cp.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(state, &cp.State); err != nil {
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
// stable primary key id, giving a deterministic order that matches the SQLite
// adapter.
func (s *Store) ListTracesPaged(ctx context.Context, sessionID string, limit int, cursor string) (*storage.TracePage, error) {
	limit = storage.ClampLimit(limit)
	afterID, err := storage.DecodeStrCursor(cursor)
	if err != nil {
		return nil, err
	}

	tenant := storage.TenantFromContext(ctx)
	const cols = `id, tenant_id, session_id, parent_id, name, kind, input, output, error, started_at, ended_at`
	var r *sql.Rows
	if afterID == "" {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM traces WHERE tenant_id=$1 AND session_id=$2 ORDER BY id LIMIT $3`,
			tenant, sessionID, limit+1,
		)
	} else {
		r, err = s.db.QueryContext(ctx,
			`SELECT `+cols+` FROM traces WHERE tenant_id=$1 AND session_id=$2 AND id>$3 ORDER BY id LIMIT $4`,
			tenant, sessionID, afterID, limit+1,
		)
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	page := &storage.TracePage{}
	for r.Next() {
		t := &storage.Trace{}
		var inp, outp []byte
		if err := r.Scan(&t.ID, &t.TenantID, &t.SessionID, &t.ParentID, &t.Name, &t.Kind, &inp, &outp, &t.Error, &t.StartedAt, &t.EndedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(inp, &t.Input); err != nil {
			return nil, fmt.Errorf("unmarshal trace input: %w", err)
		}
		if err := json.Unmarshal(outp, &t.Output); err != nil {
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

// --- Retention ---
//
// Age-based trimming compares instants. All timestamp columns are TIMESTAMPTZ,
// which PostgreSQL stores and compares as absolute UTC instants regardless of the
// client timezone, so a direct `created_at < $1` bound is reliable. (The SQLite
// adapter must additionally wrap both sides in julianday() because go-sqlite3
// stores timestamps as timezone-tagged text; see its scale.go for details.)

// TrimEvents deletes events created strictly before olderThan.
func (s *Store) TrimEvents(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE created_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim events: %w", err)
	}
	return res.RowsAffected()
}

// TrimTraces deletes traces started strictly before olderThan.
func (s *Store) TrimTraces(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM traces WHERE started_at < $1`, olderThan)
	if err != nil {
		return 0, fmt.Errorf("trim traces: %w", err)
	}
	return res.RowsAffected()
}

// TrimAuditLogs deletes audit logs created strictly before olderThan.
func (s *Store) TrimAuditLogs(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at < $1`, olderThan)
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
	tenant := storage.TenantFromContext(ctx)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM checkpoints WHERE tenant_id=$1 AND session_id=$2 AND seq_num NOT IN (
			SELECT seq_num FROM checkpoints WHERE tenant_id=$1 AND session_id=$2 ORDER BY seq_num DESC LIMIT $3
		)`,
		tenant, sessionID, keep,
	)
	if err != nil {
		return 0, fmt.Errorf("trim checkpoints: %w", err)
	}
	return res.RowsAffected()
}

// --- Batch ingestion ---

// AppendEvents appends many events within one transaction. It is idempotent on
// event id (ON CONFLICT DO NOTHING). The batch is split into chunks so the
// per-statement bind-parameter count stays under pgMaxBindParams; all chunks run
// in the same transaction, so the whole batch still commits (or rolls back)
// atomically.
func (s *Store) AppendEvents(ctx context.Context, events []*storage.Event) error {
	if len(events) == 0 {
		return nil
	}
	tenant := storage.TenantFromContext(ctx)
	const paramsPerRow = 7
	rowsPerChunk := pgMaxBindParams / paramsPerRow

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
		b.WriteString(`INSERT INTO events (id, tenant_id, session_id, seq_num, type, payload, created_at) VALUES `)
		args := make([]any, 0, len(chunk)*paramsPerRow)
		n := 0
		for i, e := range chunk {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)", n+1, n+2, n+3, n+4, n+5, n+6, n+7)
			n += paramsPerRow
			payload, err := json.Marshal(e.Payload)
			if err != nil {
				return fmt.Errorf("marshal event payload: %w", err)
			}
			args = append(args, e.ID, tenant, e.SessionID, e.SeqNum, e.Type, payload, e.CreatedAt)
		}
		b.WriteString(` ON CONFLICT (id) DO NOTHING`)
		if _, err := tx.ExecContext(ctx, b.String(), args...); err != nil {
			return fmt.Errorf("batch append events: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit batch events: %w", err)
	}
	return nil
}

// InsertTraces bulk-loads many trace spans using PostgreSQL's COPY protocol via
// the pgx driver, which is materially faster than per-row INSERTs at scale.
func (s *Store) InsertTraces(ctx context.Context, traces []*storage.Trace) error {
	if len(traces) == 0 {
		return nil
	}
	tenant := storage.TenantFromContext(ctx)
	rows := make([][]any, len(traces))
	for i, t := range traces {
		inp, _ := json.Marshal(t.Input)
		outp, _ := json.Marshal(t.Output)
		rows[i] = []any{
			t.ID, tenant, t.SessionID, t.ParentID, t.Name, t.Kind,
			string(inp), string(outp), t.Error, t.StartedAt, t.EndedAt,
		}
	}
	columns := []string{"id", "tenant_id", "session_id", "parent_id", "name", "kind", "input", "output", "error", "started_at", "ended_at"}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer func() { _ = conn.Close() }()

	return conn.Raw(func(driverConn any) error {
		pgxConn, ok := driverConn.(*stdlib.Conn)
		if !ok {
			return fmt.Errorf("batch insert traces: driver conn is not pgx")
		}
		_, err := pgxConn.Conn().CopyFrom(ctx, pgx.Identifier{"traces"}, columns, pgx.CopyFromRows(rows))
		if err != nil {
			return fmt.Errorf("batch insert traces: %w", err)
		}
		return nil
	})
}
