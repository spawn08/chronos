// Package migrate provides versioned database migrations for SQL backends.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dialect identifies the SQL dialect a Migrator targets. It controls placeholder
// syntax (`?` vs `$N`) for the migrator's own bookkeeping statements and whether
// a cross-process advisory lock is available.
type Dialect int

const (
	// DialectSQLite uses `?` placeholders and a no-op advisory lock (SQLite is
	// single-writer, so concurrent migrators cannot corrupt each other).
	DialectSQLite Dialect = iota
	// DialectPostgres uses `$N` placeholders and a pg_advisory_lock so multiple
	// processes migrating the same database serialize safely.
	DialectPostgres
)

// advisoryLockKey is a fixed 64-bit key for pg_advisory_lock. All Chronos
// migrators share it so they serialize against one another.
const advisoryLockKey int64 = 0x6368726f6e6f736d // "chronosm"

// Migration represents a single versioned migration.
type Migration struct {
	Version     int
	Description string
	Up          string // SQL to apply
	Down        string // SQL to roll back
}

// Migrator manages versioned migrations for a SQL database.
type Migrator struct {
	db         *sql.DB
	dialect    Dialect
	migrations []Migration
}

// Option configures a Migrator.
type Option func(*Migrator)

// WithDialect sets the SQL dialect. The default is DialectSQLite.
func WithDialect(d Dialect) Option {
	return func(m *Migrator) { m.dialect = d }
}

// New creates a new Migrator for the given database connection. Without options
// it targets SQLite; pass WithDialect(DialectPostgres) for PostgreSQL.
func New(db *sql.DB, opts ...Option) *Migrator {
	m := &Migrator{db: db, dialect: DialectSQLite}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Add registers a migration. Migrations are applied in version order.
func (m *Migrator) Add(version int, description, up, down string) *Migrator {
	m.migrations = append(m.migrations, Migration{
		Version:     version,
		Description: description,
		Up:          up,
		Down:        down,
	})
	return m
}

// querier is satisfied by both *sql.DB and *sql.Conn, letting migration
// bookkeeping run on a single pinned connection while holding an advisory lock.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// rebind rewrites `?` placeholders to `$N` for Postgres; SQLite is unchanged.
func (m *Migrator) rebind(query string) string {
	if m.dialect != DialectPostgres {
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

// lock acquires the advisory lock (Postgres) or is a no-op (SQLite). The lock is
// session-scoped, so it must be acquired and released on the same pinned conn.
func (m *Migrator) lock(ctx context.Context, conn *sql.Conn) error {
	if m.dialect != DialectPostgres {
		return nil
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("migrate: acquire advisory lock: %w", err)
	}
	return nil
}

// unlock releases the advisory lock (Postgres) or is a no-op (SQLite).
func (m *Migrator) unlock(ctx context.Context, conn *sql.Conn) {
	if m.dialect != DialectPostgres {
		return
	}
	_, _ = conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
}

// Migrate applies all pending migrations. Under Postgres it holds a
// pg_advisory_lock for the duration so concurrent migrators serialize.
func (m *Migrator) Migrate(ctx context.Context) error {
	conn, connErr := m.db.Conn(ctx)
	if connErr != nil {
		return fmt.Errorf("migrate: acquire conn: %w", connErr)
	}
	defer func() { _ = conn.Close() }()

	if err := m.lock(ctx, conn); err != nil {
		return err
	}
	defer m.unlock(ctx, conn)

	if err := m.ensureTable(ctx, conn); err != nil {
		return err
	}

	current, err := m.currentVersion(ctx, conn)
	if err != nil {
		return err
	}

	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})

	for _, mig := range m.migrations {
		if mig.Version <= current {
			continue
		}
		if err := m.apply(ctx, conn, mig); err != nil {
			return fmt.Errorf("migrate v%d (%s): %w", mig.Version, mig.Description, err)
		}
	}

	return nil
}

// Rollback reverts the last applied migration.
func (m *Migrator) Rollback(ctx context.Context) error {
	if err := m.ensureTable(ctx, m.db); err != nil {
		return err
	}

	current, err := m.currentVersion(ctx, m.db)
	if err != nil {
		return err
	}
	if current == 0 {
		return fmt.Errorf("migrate: no migrations to roll back")
	}

	// Find the migration to roll back
	for _, mig := range m.migrations {
		if mig.Version == current {
			if mig.Down == "" {
				return fmt.Errorf("migrate v%d: no rollback SQL defined", mig.Version)
			}
			if _, err := m.db.ExecContext(ctx, mig.Down); err != nil {
				return fmt.Errorf("migrate rollback v%d: %w", mig.Version, err)
			}
			_, err := m.db.ExecContext(ctx,
				m.rebind(`DELETE FROM _migrations WHERE version = ?`), mig.Version)
			return err
		}
	}

	return fmt.Errorf("migrate: migration v%d not found in registry", current)
}

// MigrationStatus reports the current migration version plus applied and pending
// migrations.
type MigrationStatus struct {
	CurrentVersion int                `json:"current_version"`
	Applied        []AppliedMigration `json:"applied"`
	Pending        []Migration        `json:"pending"`
}

// AppliedMigration records a migration that has been applied.
type AppliedMigration struct {
	Version     int       `json:"version"`
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
}

// Status returns the current migration version and the applied/pending lists.
func (m *Migrator) Status(ctx context.Context) (*MigrationStatus, error) {
	if err := m.ensureTable(ctx, m.db); err != nil {
		return nil, err
	}

	rows, err := m.db.QueryContext(ctx,
		`SELECT version, description, applied_at FROM _migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("migrate status: %w", err)
	}
	defer rows.Close()

	var applied []AppliedMigration
	appliedSet := make(map[int]bool)
	for rows.Next() {
		var a AppliedMigration
		if err := rows.Scan(&a.Version, &a.Description, &a.AppliedAt); err != nil {
			return nil, fmt.Errorf("migrate status scan: %w", err)
		}
		applied = append(applied, a)
		appliedSet[a.Version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate status rows: %w", err)
	}

	var pending []Migration
	for _, mig := range m.migrations {
		if !appliedSet[mig.Version] {
			pending = append(pending, mig)
		}
	}

	current := 0
	if len(applied) > 0 {
		current = applied[len(applied)-1].Version
	}

	return &MigrationStatus{
		CurrentVersion: current,
		Applied:        applied,
		Pending:        pending,
	}, nil
}

func (m *Migrator) ensureTable(ctx context.Context, q querier) error {
	_, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("migrate: creating migrations table: %w", err)
	}
	return nil
}

func (m *Migrator) currentVersion(ctx context.Context, q querier) (int, error) {
	var version int
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM _migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("migrate: getting current version: %w", err)
	}
	return version, nil
}

func (m *Migrator) apply(ctx context.Context, q querier, mig Migration) error {
	tx, err := q.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, mig.Up); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		m.rebind(`INSERT INTO _migrations (version, description, applied_at) VALUES (?, ?, ?)`),
		mig.Version, mig.Description, time.Now()); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return tx.Commit()
}
