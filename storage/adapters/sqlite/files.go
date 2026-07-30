package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Compile-time check that Store implements the optional session file store.
var _ storage.SessionFileStore = (*Store)(nil)

// PutFile creates or overwrites the session file at path.
func (s *Store) PutFile(ctx context.Context, sessionID, path string, content []byte) error {
	tenant := storage.TenantFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO session_files (tenant_id, session_id, path, content, size, updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT (tenant_id, session_id, path) DO UPDATE SET content=excluded.content, size=excluded.size, updated_at=excluded.updated_at`,
		tenant, sessionID, path, content, len(content), time.Now(),
	)
	if err != nil {
		return fmt.Errorf("sqlite put file %q: %w", path, err)
	}
	return nil
}

// GetFile returns the content of the session file at path.
func (s *Store) GetFile(ctx context.Context, sessionID, path string) ([]byte, error) {
	tenant := storage.TenantFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT content FROM session_files WHERE tenant_id=? AND session_id=? AND path=?`,
		tenant, sessionID, path,
	)
	var content []byte
	if err := row.Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("sqlite get file %q: %w", path, storage.ErrFileNotFound)
		}
		return nil, fmt.Errorf("sqlite get file %q: %w", path, err)
	}
	return content, nil
}

// ListFiles returns metadata for every session file whose path has the given
// prefix, ordered by path.
func (s *Store) ListFiles(ctx context.Context, sessionID, prefix string) ([]storage.FileInfo, error) {
	tenant := storage.TenantFromContext(ctx)
	// Byte-wise range scan (SQLite's default BINARY collation is case-sensitive),
	// avoiding LIKE whose SQLite semantics are case-insensitive.
	lo, hi, hasUpper := storage.PrefixRange(prefix)
	q := `SELECT path, size, updated_at FROM session_files
		 WHERE tenant_id=? AND session_id=? AND path >= ?`
	args := []any{tenant, sessionID, lo}
	if hasUpper {
		q += ` AND path < ?`
		args = append(args, hi)
	}
	q += ` ORDER BY path LIMIT ?`
	args = append(args, storage.MaxPageLimit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite list files: %w", err)
	}
	defer rows.Close()
	var out []storage.FileInfo
	for rows.Next() {
		var fi storage.FileInfo
		if err := rows.Scan(&fi.Path, &fi.Size, &fi.UpdatedAt); err != nil {
			return nil, fmt.Errorf("sqlite list files: scan: %w", err)
		}
		out = append(out, fi)
	}
	return out, rows.Err()
}

// DeleteFile removes the session file at path; deleting a missing file is a
// no-op.
func (s *Store) DeleteFile(ctx context.Context, sessionID, path string) error {
	tenant := storage.TenantFromContext(ctx)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM session_files WHERE tenant_id=? AND session_id=? AND path=?`,
		tenant, sessionID, path,
	)
	if err != nil {
		return fmt.Errorf("sqlite delete file %q: %w", path, err)
	}
	return nil
}
