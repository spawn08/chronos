package scheduler

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
	// DialectSQLite targets SQLite (dev/test). Writers are serialized, so the
	// atomic conditional advance in ClaimDue is exactly-once without SKIP LOCKED.
	DialectSQLite Dialect = "sqlite"
	// DialectPostgres targets PostgreSQL (production). ClaimDue selects due rows
	// FOR UPDATE SKIP LOCKED so many replicas claim disjoint schedules.
	DialectPostgres Dialect = "postgres"
)

const schedCols = "id, agent_id, cron_expr, input, new_session, session_id, enabled, " +
	"created_at, last_run_at, next_run_at, run_count"

// SQLStore implements Store over any database/sql database (SQLite or
// PostgreSQL, selected by Dialect). It owns the scheduler schema and never
// touches the shared storage package's tables.
type SQLStore struct {
	db      *sql.DB
	dialect Dialect
}

// NewSQLStore constructs a store over an already-open *sql.DB. The caller owns
// the DB lifecycle unless Close is used.
func NewSQLStore(db *sql.DB, dialect Dialect) *SQLStore {
	return &SQLStore{db: db, dialect: dialect}
}

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

// Close closes the underlying database.
func (s *SQLStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("scheduler store close: %w", err)
	}
	return nil
}

// Migrate creates the scheduler schema.
func (s *SQLStore) Migrate(ctx context.Context) error {
	ts := "TIMESTAMP"
	if s.dialect == DialectPostgres {
		ts = "TIMESTAMPTZ"
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS scheduler_schedules (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			cron_expr TEXT NOT NULL,
			input TEXT NOT NULL DEFAULT '',
			new_session INTEGER NOT NULL DEFAULT 0,
			session_id TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at ` + ts + ` NOT NULL,
			last_run_at ` + ts + `,
			next_run_at ` + ts + `,
			run_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_due ON scheduler_schedules(enabled, next_run_at)`,
		`CREATE TABLE IF NOT EXISTS scheduler_history (
			id TEXT PRIMARY KEY,
			schedule_id TEXT NOT NULL,
			agent_id TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			input TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			started_at ` + ts + ` NOT NULL,
			finished_at ` + ts + `
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduler_history ON scheduler_history(schedule_id, started_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("scheduler migrate: %w", err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Add persists a new schedule.
func (s *SQLStore) Add(ctx context.Context, sched *Schedule) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO scheduler_schedules (`+schedCols+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		sched.ID, sched.AgentID, sched.CronExpr, sched.Input, boolToInt(sched.NewSession),
		sched.SessionID, boolToInt(sched.Enabled), sched.CreatedAt.UTC(),
		nullTime(sched.LastRunAt), nullTime(sched.NextRunAt), sched.RunCount)
	if err != nil {
		return fmt.Errorf("scheduler add: %w", err)
	}
	return nil
}

// Remove deletes a schedule by ID.
func (s *SQLStore) Remove(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, s.rebind(`DELETE FROM scheduler_schedules WHERE id=?`), id)
	if err != nil {
		return fmt.Errorf("scheduler remove: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("scheduler remove: rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("scheduler: schedule %q not found", id)
	}
	return nil
}

// Get returns a schedule by ID.
func (s *SQLStore) Get(ctx context.Context, id string) (*Schedule, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT `+schedCols+` FROM scheduler_schedules WHERE id=?`), id)
	sched, err := scanSchedule(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("scheduler: schedule %q not found", id)
		}
		return nil, fmt.Errorf("scheduler get: %w", err)
	}
	return sched, nil
}

// List returns all schedules.
func (s *SQLStore) List(ctx context.Context) ([]*Schedule, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT `+schedCols+` FROM scheduler_schedules ORDER BY created_at ASC`))
	if err != nil {
		return nil, fmt.Errorf("scheduler list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Schedule
	for rows.Next() {
		sched, err := scanSchedule(rows)
		if err != nil {
			return nil, fmt.Errorf("scheduler list: scan: %w", err)
		}
		out = append(out, sched)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler list: rows: %w", err)
	}
	return out, nil
}

// ClaimDue atomically advances due schedules and returns those claimed by this
// caller. The winning replica is the one whose conditional UPDATE affects the
// row: after it advances next_run_at into the future, a concurrent replica's
// UPDATE re-evaluates the `next_run_at <= now` guard against the already-advanced
// row and affects zero rows, so the firing is claimed exactly once.
func (s *SQLStore) ClaimDue(ctx context.Context, now time.Time, nextFn func(expr string, after time.Time) time.Time) ([]*Schedule, error) {
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("scheduler claim: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	selectQ := `SELECT ` + schedCols + ` FROM scheduler_schedules
		WHERE enabled=1 AND next_run_at IS NOT NULL AND next_run_at <= ?
		ORDER BY next_run_at ASC`
	if s.dialect == DialectPostgres {
		selectQ += " FOR UPDATE SKIP LOCKED"
	}

	rows, err := tx.QueryContext(ctx, s.rebind(selectQ), now)
	if err != nil {
		return nil, fmt.Errorf("scheduler claim: select: %w", err)
	}
	var candidates []*Schedule
	for rows.Next() {
		sched, scanErr := scanSchedule(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scheduler claim: scan: %w", scanErr)
		}
		candidates = append(candidates, sched)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("scheduler claim: rows: %w", err)
	}
	_ = rows.Close()

	var claimed []*Schedule
	for _, sched := range candidates {
		next := nextFn(sched.CronExpr, now)
		res, upErr := tx.ExecContext(ctx, s.rebind(
			`UPDATE scheduler_schedules
			 SET next_run_at=?, last_run_at=?, run_count=run_count+1
			 WHERE id=? AND enabled=1 AND next_run_at IS NOT NULL AND next_run_at <= ?`),
			nullTime(next), now, sched.ID, now)
		if upErr != nil {
			return nil, fmt.Errorf("scheduler claim: advance: %w", upErr)
		}
		n, raErr := res.RowsAffected()
		if raErr != nil {
			return nil, fmt.Errorf("scheduler claim: rows affected: %w", raErr)
		}
		if n == 1 {
			sched.NextRunAt = next
			sched.LastRunAt = now
			sched.RunCount++
			claimed = append(claimed, sched)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("scheduler claim: commit: %w", err)
	}
	return claimed, nil
}

// SetSession persists the reused session ID for a schedule.
func (s *SQLStore) SetSession(ctx context.Context, id, sessionID string) error {
	if _, err := s.db.ExecContext(ctx, s.rebind(
		`UPDATE scheduler_schedules SET session_id=? WHERE id=?`), sessionID, id); err != nil {
		return fmt.Errorf("scheduler set session: %w", err)
	}
	return nil
}

// AddRunRecord appends a run record.
func (s *SQLStore) AddRunRecord(ctx context.Context, rec RunRecord) error {
	_, err := s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO scheduler_history (id, schedule_id, agent_id, session_id, input, status, error, started_at, finished_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		rec.ID, rec.ScheduleID, rec.AgentID, rec.SessionID, rec.Input, rec.Status, rec.Error,
		rec.StartedAt.UTC(), nullTime(rec.FinishedAt))
	if err != nil {
		return fmt.Errorf("scheduler add run record: %w", err)
	}
	return nil
}

// History returns run records for a schedule.
func (s *SQLStore) History(ctx context.Context, scheduleID string) ([]RunRecord, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT id, schedule_id, agent_id, session_id, input, status, error, started_at, finished_at
		 FROM scheduler_history WHERE schedule_id=? ORDER BY started_at ASC`), scheduleID)
	if err != nil {
		return nil, fmt.Errorf("scheduler history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []RunRecord
	for rows.Next() {
		var (
			rec      RunRecord
			started  time.Time
			finished sql.NullTime
		)
		if err := rows.Scan(&rec.ID, &rec.ScheduleID, &rec.AgentID, &rec.SessionID, &rec.Input,
			&rec.Status, &rec.Error, &started, &finished); err != nil {
			return nil, fmt.Errorf("scheduler history: scan: %w", err)
		}
		rec.StartedAt = started
		if finished.Valid {
			rec.FinishedAt = finished.Time
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scheduler history: rows: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSchedule(sc rowScanner) (*Schedule, error) {
	var (
		sched      Schedule
		newSession int
		enabled    int
		created    time.Time
		lastRun    sql.NullTime
		nextRun    sql.NullTime
	)
	if err := sc.Scan(&sched.ID, &sched.AgentID, &sched.CronExpr, &sched.Input, &newSession,
		&sched.SessionID, &enabled, &created, &lastRun, &nextRun, &sched.RunCount); err != nil {
		return nil, err
	}
	sched.NewSession = newSession != 0
	sched.Enabled = enabled != 0
	sched.CreatedAt = created
	if lastRun.Valid {
		sched.LastRunAt = lastRun.Time
	}
	if nextRun.Valid {
		sched.NextRunAt = nextRun.Time
	}
	return &sched, nil
}

// nullTime maps a zero time.Time to SQL NULL and otherwise normalizes to UTC.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}
