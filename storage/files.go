package storage

import (
	"context"
	"errors"
	"time"
)

// ErrFileNotFound is returned by SessionFileStore.GetFile when no file exists at
// the requested path. Callers can test for it with errors.Is.
var ErrFileNotFound = errors.New("session file not found")

// PrefixRange returns the half-open key range [lo, hi) containing exactly the
// strings that begin with prefix, for a byte-wise (case-sensitive) SQL range
// scan of the form `path >= lo AND path < hi`. A range scan is used in place of
// SQL LIKE deliberately: SQLite's LIKE is ASCII-case-insensitive by default,
// which would make prefix listing diverge from the case-sensitive Postgres and
// in-memory implementations; a byte range is case-sensitive on every backend.
//
// hasUpper is false when no finite upper bound exists — an empty prefix (lists
// everything) or a prefix that is entirely 0xFF bytes — in which case the caller
// applies only the `path >= lo` bound.
func PrefixRange(prefix string) (lo, hi string, hasUpper bool) {
	lo = prefix
	b := []byte(prefix)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] != 0xFF {
			b[i]++
			return lo, string(b[:i+1]), true
		}
	}
	return lo, "", false
}

// FileInfo describes a stored session file without its content, for directory
// listings.
type FileInfo struct {
	Path      string    `json:"path"`
	Size      int       `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionFileStore is an OPTIONAL interface a Storage adapter may implement to
// persist per-session scratch files — the backing store for the harness virtual
// filesystem (engine/tool/builtins VFS) that lets an agent offload large
// intermediate artifacts out of its context window and page them back in by
// path. It is deliberately separate from Storage so that adding it never breaks
// existing Storage implementations; callers type-assert:
//
//	if fs, ok := store.(storage.SessionFileStore); ok {
//		_ = fs.PutFile(ctx, sessionID, "notes.md", data)
//	}
//
// Every method is scoped to the tenant carried by ctx (TenantFromContext) and to
// the given session id, so files written under one tenant/session are never
// visible to another — the same isolation guarantee as the rest of Storage.
type SessionFileStore interface {
	// PutFile creates or overwrites the file at path with content.
	PutFile(ctx context.Context, sessionID, path string, content []byte) error
	// GetFile returns the content of the file at path, or an error if it does
	// not exist.
	GetFile(ctx context.Context, sessionID, path string) ([]byte, error)
	// ListFiles returns metadata for every file in the session whose path has
	// the given prefix (empty prefix lists all), ordered by path.
	ListFiles(ctx context.Context, sessionID, prefix string) ([]FileInfo, error)
	// DeleteFile removes the file at path. Deleting a missing file is not an
	// error (idempotent).
	DeleteFile(ctx context.Context, sessionID, path string) error
}
