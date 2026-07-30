package postgres

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
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (tenant_id, session_id, path) DO UPDATE SET content=EXCLUDED.content, size=EXCLUDED.size, updated_at=EXCLUDED.updated_at`,
		tenant, sessionID, path, content, len(content), time.Now(),
	)
	if err != nil {
		return fmt.Errorf("postgres put file %q: %w", path, err)
	}
	return nil
}

// GetFile returns the content of the session file at path.
func (s *Store) GetFile(ctx context.Context, sessionID, path string) ([]byte, error) {
	tenant := storage.TenantFromContext(ctx)
	row := s.db.QueryRowContext(ctx,
		`SELECT content FROM session_files WHERE tenant_id=$1 AND session_id=$2 AND path=$3`,
		tenant, sessionID, path,
	)
	var content []byte
	if err := row.Scan(&content); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("postgres get file %q: %w", path, storage.ErrFileNotFound)
		}
		return nil, fmt.Errorf("postgres get file %q: %w", path, err)
	}
	return content, nil
}

// ListFiles returns metadata for every session file whose path has the given
// prefix, ordered by path.
func (s *Store) ListFiles(ctx context.Context, sessionID, prefix string) ([]storage.FileInfo, error) {
	tenant := storage.TenantFromContext(ctx)
	// Byte-wise range scan forced to the C collation so prefix matching and
	// ordering are case-sensitive regardless of the database's default
	// collation — matching the SQLite and in-memory implementations exactly.
	lo, hi, hasUpper := storage.PrefixRange(prefix)
	q := `SELECT path, size, updated_at FROM session_files
		 WHERE tenant_id=$1 AND session_id=$2 AND path >= $3 COLLATE "C"`
	args := []any{tenant, sessionID, lo}
	n := 4
	if hasUpper {
		q += fmt.Sprintf(` AND path < $%d COLLATE "C"`, n)
		args = append(args, hi)
		n++
	}
	q += fmt.Sprintf(` ORDER BY path COLLATE "C" LIMIT $%d`, n)
	args = append(args, storage.MaxPageLimit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres list files: %w", err)
	}
	defer rows.Close()
	var out []storage.FileInfo
	for rows.Next() {
		var fi storage.FileInfo
		if err := rows.Scan(&fi.Path, &fi.Size, &fi.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres list files: scan: %w", err)
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
		`DELETE FROM session_files WHERE tenant_id=$1 AND session_id=$2 AND path=$3`,
		tenant, sessionID, path,
	)
	if err != nil {
		return fmt.Errorf("postgres delete file %q: %w", path, err)
	}
	return nil
}
