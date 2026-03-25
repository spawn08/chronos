// Package migrate provides versioned database migrations for SQL backends.
package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

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
	migrations []Migration
}

// New creates a new Migrator for the given database connection.
func New(db *sql.DB) *Migrator {
	return &Migrator{
		db: db,
	}
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

// Migrate applies all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	if err := m.ensureTable(ctx); err != nil {
		return err
	}

	current, err := m.currentVersion(ctx)
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
		if err := m.apply(ctx, mig); err != nil {
			return fmt.Errorf("migrate v%d (%s): %w", mig.Version, mig.Description, err)
		}
	}

	return nil
}

// Rollback reverts the last applied migration.
func (m *Migrator) Rollback(ctx context.Context) error {
	if err := m.ensureTable(ctx); err != nil {
		return err
	}

	current, err := m.currentVersion(ctx)
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
				`DELETE FROM _migrations WHERE version = ?`, mig.Version)
			return err
		}
	}

	return fmt.Errorf("migrate: migration v%d not found in registry", current)
}

// Status returns the current migration version and list of applied migrations.
type MigrationStatus struct {
	CurrentVersion int                `json:"current_version"`
	Applied        []AppliedMigration `json:"applied"`
	Pending        []Migration        `json:"pending"`
}

type AppliedMigration struct {
	Version     int       `json:"version"`
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
}

func (m *Migrator) Status(ctx context.Context) (*MigrationStatus, error) {
	if err := m.ensureTable(ctx); err != nil {
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

func (m *Migrator) ensureTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("migrate: creating migrations table: %w", err)
	}
	return nil
}

func (m *Migrator) currentVersion(ctx context.Context) (int, error) {
	var version int
	err := m.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM _migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("migrate: getting current version: %w", err)
	}
	return version, nil
}

func (m *Migrator) apply(ctx context.Context, mig Migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, mig.Up); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO _migrations (version, description, applied_at) VALUES (?, ?, ?)`,
		mig.Version, mig.Description, time.Now()); err != nil {
		return fmt.Errorf("record: %w", err)
	}

	return tx.Commit()
}
