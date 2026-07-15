package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dialect selects SQL syntax for the target database.
type Dialect string

const (
	// DialectSQLite targets SQLite (dev/test). Dequeue uses an atomic UPDATE over
	// a LIMIT-1 subquery; SQLite serializes writers so no SKIP LOCKED is needed.
	DialectSQLite Dialect = "sqlite"
	// DialectPostgres targets PostgreSQL (production). Dequeue uses
	// FOR UPDATE SKIP LOCKED so many workers claim disjoint runs concurrently.
	DialectPostgres Dialect = "postgres"
)

// runCols is the canonical column list for queue_runs, used by every SELECT and
// RETURNING clause so scanRun can rely on a fixed order.
const runCols = "id, session_id, graph_id, kind, payload, status, priority, attempts, " +
	"max_attempts, lease_owner, lease_expires_at, available_at, wait_signal, " +
	"signal_payload, idempotency_key, last_error, created_at, updated_at"

// SQLStore implements Store over any database/sql database. It supports SQLite
// and PostgreSQL via the Dialect. It defines and owns the queue's own schema and
// never touches the shared storage package's tables.
type SQLStore struct {
	db      *sql.DB
	dialect Dialect
}

// NewSQLStore constructs a store over an already-open *sql.DB. The caller owns
// the DB lifecycle unless Close is used.
func NewSQLStore(db *sql.DB, dialect Dialect) *SQLStore {
	return &SQLStore{db: db, dialect: dialect}
}

// rebind converts positional "?" placeholders to "$N" for Postgres.
func (s *SQLStore) rebind(query string) string {
	if s.dialect != DialectPostgres {
		return query
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (s *SQLStore) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, s.rebind(query), args...)
}

// Close closes the underlying database.
func (s *SQLStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("queue store close: %w", err)
	}
	return nil
}

// Migrate creates the queue schema.
func (s *SQLStore) Migrate(ctx context.Context) error {
	for _, stmt := range s.schema() {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("queue migrate: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) schema() []string {
	blob, ts := "BLOB", "TIMESTAMP"
	if s.dialect == DialectPostgres {
		blob, ts = "BYTEA", "TIMESTAMPTZ"
	}
	return []string{
		`CREATE TABLE IF NOT EXISTS queue_runs (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			graph_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			payload ` + blob + `,
			status TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			max_attempts INTEGER NOT NULL DEFAULT 5,
			lease_owner TEXT NOT NULL DEFAULT '',
			lease_expires_at ` + ts + `,
			available_at ` + ts + ` NOT NULL,
			wait_signal TEXT NOT NULL DEFAULT '',
			signal_payload ` + blob + `,
			idempotency_key TEXT NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at ` + ts + ` NOT NULL,
			updated_at ` + ts + ` NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_runs_claim ON queue_runs(status, available_at)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_runs_lease ON queue_runs(status, lease_expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_runs_park ON queue_runs(status, session_id, wait_signal)`,
		`CREATE TABLE IF NOT EXISTS queue_signals (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			name TEXT NOT NULL,
			payload ` + blob + `,
			consumed INTEGER NOT NULL DEFAULT 0,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_signals ON queue_signals(session_id, name, consumed)`,
		`CREATE TABLE IF NOT EXISTS queue_idempotency (
			key TEXT PRIMARY KEY,
			created_at ` + ts + ` NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS queue_outbox (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL,
			topic TEXT NOT NULL,
			payload ` + blob + `,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at ` + ts + ` NOT NULL,
			sent_at ` + ts + `
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS uq_queue_outbox_idem ON queue_outbox(idempotency_key)`,
		`CREATE INDEX IF NOT EXISTS idx_queue_outbox_status ON queue_outbox(status, created_at)`,
	}
}

// utc normalizes a time to UTC so cross-dialect string/timestamp comparisons are
// consistent (SQLite compares DATETIME values textually).
func utc(t time.Time) time.Time { return t.UTC() }

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRun(sc rowScanner) (*Run, error) {
	var (
		r       Run
		lease   sql.NullTime
		payload []byte
		sigPay  []byte
		avail   time.Time
		created time.Time
		updated time.Time
	)
	if err := sc.Scan(
		&r.ID, &r.SessionID, &r.GraphID, &r.Kind, &payload, &r.Status, &r.Priority,
		&r.Attempts, &r.MaxAttempts, &r.LeaseOwner, &lease, &avail, &r.WaitSignal,
		&sigPay, &r.IdempotencyKey, &r.LastError, &created, &updated,
	); err != nil {
		return nil, err
	}
	r.Payload = payload
	r.SignalPayload = sigPay
	r.AvailableAt = avail
	r.CreatedAt = created
	r.UpdatedAt = updated
	if lease.Valid {
		t := lease.Time
		r.LeaseExpiresAt = &t
	}
	return &r, nil
}

// EnqueueRun inserts a run.
func (s *SQLStore) EnqueueRun(ctx context.Context, r *Run) error {
	var lease any
	if r.LeaseExpiresAt != nil {
		lease = utc(*r.LeaseExpiresAt)
	}
	_, err := s.exec(ctx, `INSERT INTO queue_runs (`+runCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.SessionID, r.GraphID, r.Kind, r.Payload, r.Status, r.Priority,
		r.Attempts, r.MaxAttempts, r.LeaseOwner, lease, utc(r.AvailableAt),
		r.WaitSignal, r.SignalPayload, r.IdempotencyKey, r.LastError,
		utc(r.CreatedAt), utc(r.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert run: %w", err)
	}
	return nil
}

// DequeueRun atomically claims the next available run.
func (s *SQLStore) DequeueRun(ctx context.Context, owner string, lease time.Duration, now time.Time) (*Run, error) {
	now = utc(now)
	expires := now.Add(lease)
	skipLocked := ""
	if s.dialect == DialectPostgres {
		skipLocked = " FOR UPDATE SKIP LOCKED"
	}
	query := `UPDATE queue_runs
		SET status='` + StatusLeased + `', lease_owner=?, lease_expires_at=?, attempts=attempts+1, updated_at=?
		WHERE id = (
			SELECT id FROM queue_runs
			WHERE status='` + StatusPending + `' AND available_at <= ?
			ORDER BY priority DESC, available_at ASC, created_at ASC
			LIMIT 1` + skipLocked + `
		)
		RETURNING ` + runCols

	row := s.db.QueryRowContext(ctx, s.rebind(query), owner, expires, now, now)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrEmpty
		}
		return nil, fmt.Errorf("dequeue run: %w", err)
	}
	return r, nil
}

// Heartbeat extends the lease.
func (s *SQLStore) Heartbeat(ctx context.Context, runID, owner string, lease time.Duration, now time.Time) error {
	now = utc(now)
	res, err := s.exec(ctx, `UPDATE queue_runs SET lease_expires_at=?, updated_at=?
		WHERE id=? AND status='`+StatusLeased+`' AND lease_owner=?`,
		now.Add(lease), now, runID, owner)
	if err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return leaseAffected(res)
}

// CompleteRun sets a terminal status.
func (s *SQLStore) CompleteRun(ctx context.Context, runID, owner, status, lastErr string, now time.Time) error {
	res, err := s.exec(ctx, `UPDATE queue_runs
		SET status=?, last_error=?, lease_owner='', lease_expires_at=NULL, updated_at=?
		WHERE id=? AND status='`+StatusLeased+`' AND lease_owner=?`,
		status, lastErr, utc(now), runID, owner)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}
	return leaseAffected(res)
}

// RescheduleRun returns a run to pending, available at availableAt.
func (s *SQLStore) RescheduleRun(ctx context.Context, runID, owner string, availableAt time.Time, patch []byte, now time.Time) error {
	now = utc(now)
	var (
		res sql.Result
		err error
	)
	if patch != nil {
		res, err = s.exec(ctx, `UPDATE queue_runs
			SET status='`+StatusPending+`', available_at=?, payload=?, lease_owner='', lease_expires_at=NULL, updated_at=?
			WHERE id=? AND status='`+StatusLeased+`' AND lease_owner=?`,
			utc(availableAt), patch, now, runID, owner)
	} else {
		res, err = s.exec(ctx, `UPDATE queue_runs
			SET status='`+StatusPending+`', available_at=?, lease_owner='', lease_expires_at=NULL, updated_at=?
			WHERE id=? AND status='`+StatusLeased+`' AND lease_owner=?`,
			utc(availableAt), now, runID, owner)
	}
	if err != nil {
		return fmt.Errorf("reschedule run: %w", err)
	}
	return leaseAffected(res)
}

// ParkRun suspends a leased run pending a signal, honoring an already-delivered
// signal to avoid a lost-signal race.
func (s *SQLStore) ParkRun(ctx context.Context, runID, owner, waitSignal string, patch []byte, now time.Time) error {
	now = utc(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("park run: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var sessionID string
	err = tx.QueryRowContext(ctx, s.rebind(
		`SELECT session_id FROM queue_runs WHERE id=? AND status='`+StatusLeased+`' AND lease_owner=?`),
		runID, owner).Scan(&sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrLeaseLost
		}
		return fmt.Errorf("park run: load: %w", err)
	}

	// Consume an already-delivered signal if present.
	var (
		sigID  string
		sigPay []byte
	)
	err = tx.QueryRowContext(ctx, s.rebind(
		`SELECT id, payload FROM queue_signals
		 WHERE session_id=? AND name=? AND consumed=0 ORDER BY created_at ASC LIMIT 1`),
		sessionID, waitSignal).Scan(&sigID, &sigPay)
	switch {
	case err == nil:
		if _, cErr := tx.ExecContext(ctx, s.rebind(
			`UPDATE queue_signals SET consumed=1 WHERE id=?`), sigID); cErr != nil {
			return fmt.Errorf("park run: consume signal: %w", cErr)
		}
		if pErr := parkExec(ctx, tx, s.rebind, runID, StatusPending, "", sigPay, patch, now, now); pErr != nil {
			return pErr
		}
	case errors.Is(err, sql.ErrNoRows):
		if pErr := parkExec(ctx, tx, s.rebind, runID, StatusParked, waitSignal, nil, patch, time.Time{}, now); pErr != nil {
			return pErr
		}
	default:
		return fmt.Errorf("park run: check signal: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("park run: commit: %w", err)
	}
	return nil
}

// parkExec updates the run row for ParkRun. When status is pending, availableAt
// must be set; when parked, availableAt is left unchanged (zero time).
func parkExec(ctx context.Context, tx *sql.Tx, rebind func(string) string, runID, status, waitSignal string, sigPay, patch []byte, availableAt, now time.Time) error {
	set := `SET status=?, wait_signal=?, signal_payload=?, lease_owner='', lease_expires_at=NULL, updated_at=?`
	args := []any{status, waitSignal, sigPay}
	if !availableAt.IsZero() {
		set += `, available_at=?`
	}
	if patch != nil {
		set += `, payload=?`
	}
	// Assemble args in the order the SET clause references them.
	tail := []any{now}
	if !availableAt.IsZero() {
		tail = append(tail, availableAt)
	}
	if patch != nil {
		tail = append(tail, patch)
	}
	args = append(args, tail...)
	args = append(args, runID)
	if _, err := tx.ExecContext(ctx, rebind(`UPDATE queue_runs `+set+` WHERE id=?`), args...); err != nil {
		return fmt.Errorf("park run: update: %w", err)
	}
	return nil
}

// DeliverSignal wakes parked runs waiting on sig.Name; retains the signal if none.
func (s *SQLStore) DeliverSignal(ctx context.Context, sig *Signal, now time.Time) (int, error) {
	now = utc(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("deliver signal: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, s.rebind(`UPDATE queue_runs
		SET status='`+StatusPending+`', available_at=?, signal_payload=?, wait_signal='', updated_at=?
		WHERE status='`+StatusParked+`' AND session_id=? AND wait_signal=?`),
		now, sig.Payload, now, sig.SessionID, sig.Name)
	if err != nil {
		return 0, fmt.Errorf("deliver signal: wake: %w", err)
	}
	n, _ := res.RowsAffected()

	if n == 0 {
		id := fmt.Sprintf("sig_%d_%s", now.UnixNano(), sig.Name)
		if _, err := tx.ExecContext(ctx, s.rebind(
			`INSERT INTO queue_signals (id, session_id, name, payload, consumed, created_at)
			 VALUES (?, ?, ?, ?, 0, ?)`),
			id, sig.SessionID, sig.Name, sig.Payload, now); err != nil {
			return 0, fmt.Errorf("deliver signal: retain: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("deliver signal: commit: %w", err)
	}
	return int(n), nil
}

// ReleaseParked promotes parked runs back to pending.
func (s *SQLStore) ReleaseParked(ctx context.Context, waitSignal string, limit int, now time.Time) (int, error) {
	now = utc(now)
	res, err := s.exec(ctx, `UPDATE queue_runs
		SET status='`+StatusPending+`', available_at=?, wait_signal='', updated_at=?
		WHERE id IN (
			SELECT id FROM queue_runs WHERE status='`+StatusParked+`' AND wait_signal=?
			ORDER BY created_at ASC LIMIT ?
		)`,
		now, now, waitSignal, limit)
	if err != nil {
		return 0, fmt.Errorf("release parked: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RecoverOrphans re-enqueues expired-lease runs and fails exhausted ones.
func (s *SQLStore) RecoverOrphans(ctx context.Context, now time.Time) (int, error) {
	now = utc(now)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("recover orphans: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Fail runs whose retries are exhausted.
	if _, err = tx.ExecContext(ctx, s.rebind(`UPDATE queue_runs
		SET status='`+StatusFailed+`', last_error='lease expired; attempts exhausted', lease_owner='', lease_expires_at=NULL, updated_at=?
		WHERE status='`+StatusLeased+`' AND lease_expires_at < ? AND attempts >= max_attempts`),
		now, now); err != nil {
		return 0, fmt.Errorf("recover orphans: fail exhausted: %w", err)
	}

	// Re-enqueue recoverable runs.
	res, err := tx.ExecContext(ctx, s.rebind(`UPDATE queue_runs
		SET status='`+StatusPending+`', available_at=?, lease_owner='', lease_expires_at=NULL, updated_at=?
		WHERE status='`+StatusLeased+`' AND lease_expires_at < ? AND attempts < max_attempts`),
		now, now, now)
	if err != nil {
		return 0, fmt.Errorf("recover orphans: re-enqueue: %w", err)
	}
	n, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("recover orphans: commit: %w", err)
	}
	return int(n), nil
}

// CountByStatus counts runs in a status.
func (s *SQLStore) CountByStatus(ctx context.Context, status string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, s.rebind(`SELECT COUNT(*) FROM queue_runs WHERE status=?`), status).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count by status: %w", err)
	}
	return n, nil
}

// GetRun returns a run by ID.
func (s *SQLStore) GetRun(ctx context.Context, id string) (*Run, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`SELECT `+runCols+` FROM queue_runs WHERE id=?`), id)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// MarkIdempotent records key once.
func (s *SQLStore) MarkIdempotent(ctx context.Context, key string, now time.Time) (bool, error) {
	q := `INSERT INTO queue_idempotency (key, created_at) VALUES (?, ?) ON CONFLICT (key) DO NOTHING`
	if s.dialect == DialectSQLite {
		q = `INSERT OR IGNORE INTO queue_idempotency (key, created_at) VALUES (?, ?)`
	}
	res, err := s.exec(ctx, q, key, utc(now))
	if err != nil {
		return false, fmt.Errorf("mark idempotent: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// EnqueueOutbox records an external effect, ignoring duplicate idempotency keys.
func (s *SQLStore) EnqueueOutbox(ctx context.Context, e *OutboxEntry) error {
	if e.ID == "" {
		e.ID = fmt.Sprintf("obx_%d_%s", time.Now().UnixNano(), e.IdempotencyKey)
	}
	if e.Status == "" {
		e.Status = OutboxPending
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	q := `INSERT INTO queue_outbox (id, session_id, idempotency_key, topic, payload, status, attempts, last_error, created_at, sent_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL) ON CONFLICT (idempotency_key) DO NOTHING`
	if s.dialect == DialectSQLite {
		q = `INSERT OR IGNORE INTO queue_outbox (id, session_id, idempotency_key, topic, payload, status, attempts, last_error, created_at, sent_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`
	}
	_, err := s.exec(ctx, q, e.ID, e.SessionID, e.IdempotencyKey, e.Topic, e.Payload,
		e.Status, e.Attempts, e.LastError, utc(e.CreatedAt))
	if err != nil {
		return fmt.Errorf("enqueue outbox: %w", err)
	}
	return nil
}

// ClaimOutbox returns pending outbox entries.
func (s *SQLStore) ClaimOutbox(ctx context.Context, limit int) ([]*OutboxEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT id, session_id, idempotency_key, topic, payload, status, attempts, last_error, created_at, sent_at
		 FROM queue_outbox WHERE status=? ORDER BY created_at ASC LIMIT ?`),
		OutboxPending, limit)
	if err != nil {
		return nil, fmt.Errorf("claim outbox: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*OutboxEntry
	for rows.Next() {
		var (
			e    OutboxEntry
			sent sql.NullTime
		)
		if err := rows.Scan(&e.ID, &e.SessionID, &e.IdempotencyKey, &e.Topic, &e.Payload,
			&e.Status, &e.Attempts, &e.LastError, &e.CreatedAt, &sent); err != nil {
			return nil, fmt.Errorf("claim outbox: scan: %w", err)
		}
		if sent.Valid {
			t := sent.Time
			e.SentAt = &t
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim outbox: rows: %w", err)
	}
	return out, nil
}

// MarkOutboxSent marks an entry delivered.
func (s *SQLStore) MarkOutboxSent(ctx context.Context, id string, now time.Time) error {
	if _, err := s.exec(ctx, `UPDATE queue_outbox SET status=?, sent_at=? WHERE id=?`,
		OutboxSent, utc(now), id); err != nil {
		return fmt.Errorf("mark outbox sent: %w", err)
	}
	return nil
}

// MarkOutboxFailed records a delivery failure; the entry stays pending for retry.
func (s *SQLStore) MarkOutboxFailed(ctx context.Context, id, errMsg string, now time.Time) error {
	if _, err := s.exec(ctx, `UPDATE queue_outbox SET attempts=attempts+1, last_error=? WHERE id=?`,
		errMsg, id); err != nil {
		return fmt.Errorf("mark outbox failed: %w", err)
	}
	return nil
}

// leaseAffected converts a zero-row update into ErrLeaseLost.
func leaseAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}
