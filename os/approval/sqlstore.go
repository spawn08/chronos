package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Dialect selects SQL syntax for the target database.
type Dialect string

const (
	// DialectSQLite targets SQLite (dev/test).
	DialectSQLite Dialect = "sqlite"
	// DialectPostgres targets PostgreSQL (production).
	DialectPostgres Dialect = "postgres"
)

// SQLStore implements Store over any database/sql database (SQLite or
// PostgreSQL, selected by Dialect). It owns the approval schema and never
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

// Close closes the underlying database.
func (s *SQLStore) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("approval store close: %w", err)
	}
	return nil
}

// Migrate creates the approval schema.
func (s *SQLStore) Migrate(ctx context.Context) error {
	ts := "TIMESTAMP"
	if s.dialect == DialectPostgres {
		ts = "TIMESTAMPTZ"
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS approval_requests (
			id TEXT PRIMARY KEY,
			tool_name TEXT NOT NULL,
			args TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			created_at ` + ts + ` NOT NULL,
			resolved_at ` + ts + `
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approval_status ON approval_requests(status, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("approval migrate: %w", err)
		}
	}
	return nil
}

// Create records a new pending request.
func (s *SQLStore) Create(ctx context.Context, req *Request) error {
	args, err := marshalArgs(req.Args)
	if err != nil {
		return err
	}
	status := req.Status
	if status == "" {
		status = StatusPending
	}
	_, err = s.db.ExecContext(ctx, s.rebind(
		`INSERT INTO approval_requests (id, tool_name, args, status, created_at, resolved_at)
		 VALUES (?, ?, ?, ?, ?, NULL)`),
		req.ID, req.ToolName, args, status, req.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("approval create: %w", err)
	}
	return nil
}

// Resolve transitions a pending request to approved/denied.
func (s *SQLStore) Resolve(ctx context.Context, id string, approved bool, now time.Time) (*Request, error) {
	status := StatusDenied
	if approved {
		status = StatusApproved
	}
	res, err := s.db.ExecContext(ctx, s.rebind(
		`UPDATE approval_requests SET status=?, resolved_at=?
		 WHERE id=? AND status=?`),
		status, now.UTC(), id, StatusPending)
	if err != nil {
		return nil, fmt.Errorf("approval resolve: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("approval resolve: rows affected: %w", err)
	}
	if n == 0 {
		// Either the request does not exist or it was already resolved. Distinguish
		// so an idempotent re-resolve of a known request is not a 404.
		existing, getErr := s.Get(ctx, id)
		if getErr != nil {
			return nil, getErr
		}
		return existing, nil
	}
	return s.Get(ctx, id)
}

// Get returns a request by ID.
func (s *SQLStore) Get(ctx context.Context, id string) (*Request, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(
		`SELECT id, tool_name, args, status, created_at FROM approval_requests WHERE id=?`), id)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("approval get: %w", err)
	}
	return req, nil
}

// List returns all pending requests.
func (s *SQLStore) List(ctx context.Context) ([]*Request, error) {
	rows, err := s.db.QueryContext(ctx, s.rebind(
		`SELECT id, tool_name, args, status, created_at FROM approval_requests
		 WHERE status=? ORDER BY created_at ASC`), StatusPending)
	if err != nil {
		return nil, fmt.Errorf("approval list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*Request
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, fmt.Errorf("approval list: scan: %w", err)
		}
		out = append(out, req)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("approval list: rows: %w", err)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRequest(sc rowScanner) (*Request, error) {
	var (
		req     Request
		argsStr string
		created time.Time
	)
	if err := sc.Scan(&req.ID, &req.ToolName, &argsStr, &req.Status, &created); err != nil {
		return nil, err
	}
	req.CreatedAt = created
	if argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &req.Args); err != nil {
			return nil, fmt.Errorf("unmarshal args: %w", err)
		}
	}
	return &req, nil
}

func marshalArgs(args map[string]any) (string, error) {
	if args == nil {
		return "{}", nil
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("marshal args: %w", err)
	}
	return string(b), nil
}
