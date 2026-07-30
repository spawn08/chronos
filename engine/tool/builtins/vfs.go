package builtins

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spawn08/chronos/storage"
)

// VFS is a per-session, per-tenant virtual filesystem the agent uses as scratch
// space: it offloads large intermediate artifacts (research notes, drafts, tool
// output) out of the prompt and pages them back in by path on demand, keeping
// the context window small on long tasks. Paths are opaque keys within the
// session's namespace, not host filesystem paths, so there is no host access and
// no path-traversal surface.
//
// Every method resolves the session from ctx (storage.WithSession); a
// sessionless call returns ErrNoSession, identical to PlanStore, so the two
// harness primitives behave consistently.
type VFS interface {
	// Write creates or overwrites the artifact at path.
	Write(ctx context.Context, path string, content []byte) error
	// Read returns the artifact at path, or an error (storage.ErrFileNotFound)
	// if it does not exist.
	Read(ctx context.Context, path string) ([]byte, error)
	// List returns metadata for every artifact whose path has the given prefix
	// (empty prefix lists all), ordered by path.
	List(ctx context.Context, prefix string) ([]storage.FileInfo, error)
	// Delete removes the artifact at path; deleting a missing path is a no-op.
	Delete(ctx context.Context, path string) error
}

// cleanPath validates a VFS path: it trims surrounding whitespace and rejects an
// empty result. Paths are opaque keys within the session's namespace (not host
// filesystem paths), so no canonicalization is applied — "a/b", "a/b/" and
// "a//b" are distinct keys, and there is no traversal surface.
func cleanPath(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", fmt.Errorf("path must be a non-empty string")
	}
	return p, nil
}

// --- in-memory implementation ---

// InMemoryVFS is a process-local VFS keyed by (tenant, session, path). It is
// intended for tests and ephemeral use and does not survive a restart; use
// StorageVFS for durable, resume-safe artifacts.
type InMemoryVFS struct {
	mu    sync.RWMutex
	files map[string]map[string]storedFile // scope key -> path -> file
}

type storedFile struct {
	content   []byte
	updatedAt time.Time
}

// NewInMemoryVFS creates an empty in-memory virtual filesystem.
func NewInMemoryVFS() *InMemoryVFS {
	return &InMemoryVFS{files: make(map[string]map[string]storedFile)}
}

func (v *InMemoryVFS) scope(ctx context.Context) (string, error) {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return "", err
	}
	return storage.TenantFromContext(ctx) + "\x00" + sessionID, nil
}

// Write stores a copy of content at path for the context's session.
func (v *InMemoryVFS) Write(ctx context.Context, path string, content []byte) error {
	key, err := v.scope(ctx)
	if err != nil {
		return err
	}
	p, err := cleanPath(path)
	if err != nil {
		return err
	}
	stored := make([]byte, len(content))
	copy(stored, content)

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.files[key] == nil {
		v.files[key] = make(map[string]storedFile)
	}
	v.files[key][p] = storedFile{content: stored, updatedAt: time.Now()}
	return nil
}

// Read returns a copy of the artifact at path.
func (v *InMemoryVFS) Read(ctx context.Context, path string) ([]byte, error) {
	key, err := v.scope(ctx)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	f, ok := v.files[key][p]
	if !ok {
		return nil, fmt.Errorf("vfs read %q: %w", p, storage.ErrFileNotFound)
	}
	out := make([]byte, len(f.content))
	copy(out, f.content)
	return out, nil
}

// List returns metadata for artifacts with the given prefix, ordered by path.
func (v *InMemoryVFS) List(ctx context.Context, prefix string) ([]storage.FileInfo, error) {
	key, err := v.scope(ctx)
	if err != nil {
		return nil, err
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	var out []storage.FileInfo
	for path, f := range v.files[key] {
		if strings.HasPrefix(path, prefix) {
			out = append(out, storage.FileInfo{Path: path, Size: len(f.content), UpdatedAt: f.updatedAt})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Delete removes the artifact at path.
func (v *InMemoryVFS) Delete(ctx context.Context, path string) error {
	key, err := v.scope(ctx)
	if err != nil {
		return err
	}
	p, err := cleanPath(path)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.files[key], p)
	return nil
}

// --- storage-backed implementation ---

// StorageVFS is a durable VFS backed by a storage.SessionFileStore. Artifacts
// persist across process restarts and resume with the session.
type StorageVFS struct {
	files storage.SessionFileStore
}

// NewStorageVFS returns a durable VFS over store. It fails at construction (not
// at first use) when the backend does not implement storage.SessionFileStore, so
// misconfiguration surfaces immediately.
func NewStorageVFS(store storage.Storage) (*StorageVFS, error) {
	files, ok := store.(storage.SessionFileStore)
	if !ok {
		return nil, fmt.Errorf("vfs: storage backend %T does not support session files (needs storage.SessionFileStore)", store)
	}
	return &StorageVFS{files: files}, nil
}

// Write persists content at path for the context's session.
func (v *StorageVFS) Write(ctx context.Context, path string, content []byte) error {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return err
	}
	p, err := cleanPath(path)
	if err != nil {
		return err
	}
	return v.files.PutFile(ctx, sessionID, p, content)
}

// Read returns the artifact at path.
func (v *StorageVFS) Read(ctx context.Context, path string) ([]byte, error) {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return nil, err
	}
	p, err := cleanPath(path)
	if err != nil {
		return nil, err
	}
	return v.files.GetFile(ctx, sessionID, p)
}

// List returns metadata for artifacts with the given prefix.
func (v *StorageVFS) List(ctx context.Context, prefix string) ([]storage.FileInfo, error) {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return nil, err
	}
	return v.files.ListFiles(ctx, sessionID, prefix)
}

// Delete removes the artifact at path.
func (v *StorageVFS) Delete(ctx context.Context, path string) error {
	sessionID, err := sessionScope(ctx)
	if err != nil {
		return err
	}
	p, err := cleanPath(path)
	if err != nil {
		return err
	}
	return v.files.DeleteFile(ctx, sessionID, p)
}
