package builtins

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

// vfsFactory builds a fresh VFS for the substitutability suite. StorageVFS is
// backed by a file-backed SQLite store with the session pre-created.
type vfsFactory struct {
	name string
	make func(t *testing.T) VFS
}

func vfsFactories() []vfsFactory {
	return []vfsFactory{
		{"in_memory", func(_ *testing.T) VFS { return NewInMemoryVFS() }},
		{"storage", func(t *testing.T) VFS {
			// The VFS uses its own session_files table (no FK to sessions), so no
			// session row is needed — files are isolated by (tenant, session, path).
			v, err := NewStorageVFS(newDurableStore(t))
			if err != nil {
				t.Fatalf("NewStorageVFS: %v", err)
			}
			return v
		}},
	}
}

func TestVFS_RoundTripAndList(t *testing.T) {
	for _, f := range vfsFactories() {
		t.Run(f.name, func(t *testing.T) {
			vfs := f.make(t)
			ctx := storage.WithSession(context.Background(), "s1")

			if err := vfs.Write(ctx, "notes/a.md", []byte("alpha")); err != nil {
				t.Fatalf("write a: %v", err)
			}
			if err := vfs.Write(ctx, "notes/b.md", []byte("beta")); err != nil {
				t.Fatalf("write b: %v", err)
			}
			if err := vfs.Write(ctx, "draft.md", []byte("draft")); err != nil {
				t.Fatalf("write draft: %v", err)
			}

			got, err := vfs.Read(ctx, "notes/a.md")
			if err != nil || string(got) != "alpha" {
				t.Fatalf("read a = %q, %v; want alpha", got, err)
			}

			// Overwrite.
			if werr := vfs.Write(ctx, "notes/a.md", []byte("alpha2")); werr != nil {
				t.Fatalf("overwrite: %v", werr)
			}
			got, _ = vfs.Read(ctx, "notes/a.md")
			if string(got) != "alpha2" {
				t.Errorf("after overwrite = %q, want alpha2", got)
			}

			// Prefix list is ordered.
			files, err := vfs.List(ctx, "notes/")
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(files) != 2 || files[0].Path != "notes/a.md" || files[1].Path != "notes/b.md" {
				t.Errorf("list notes/ = %+v, want [notes/a.md notes/b.md]", files)
			}
			if files[0].Size != len("alpha2") {
				t.Errorf("size = %d, want %d", files[0].Size, len("alpha2"))
			}

			// Delete is idempotent.
			if err := vfs.Delete(ctx, "notes/a.md"); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if err := vfs.Delete(ctx, "notes/a.md"); err != nil {
				t.Errorf("second delete should be a no-op, got %v", err)
			}
			if _, err := vfs.Read(ctx, "notes/a.md"); !errors.Is(err, storage.ErrFileNotFound) {
				t.Errorf("read deleted = %v, want ErrFileNotFound", err)
			}
		})
	}
}

// Prefix listing is case-sensitive and identical across implementations, so
// InMemoryVFS (HasPrefix) and StorageVFS (SQL range scan) never disagree.
func TestVFS_ListPrefixCaseSensitive(t *testing.T) {
	for _, f := range vfsFactories() {
		t.Run(f.name, func(t *testing.T) {
			vfs := f.make(t)
			ctx := storage.WithSession(context.Background(), "s1")
			for _, p := range []string{"notes/lower.md", "Notes/upper.md"} {
				if err := vfs.Write(ctx, p, []byte("x")); err != nil {
					t.Fatalf("write %q: %v", p, err)
				}
			}
			files, err := vfs.List(ctx, "notes/")
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(files) != 1 || files[0].Path != "notes/lower.md" {
				t.Errorf("prefix 'notes/' = %+v, want only [notes/lower.md] (case-sensitive)", files)
			}
		})
	}
}

func TestVFS_SessionAndTenantIsolation(t *testing.T) {
	for _, f := range vfsFactories() {
		t.Run(f.name, func(t *testing.T) {
			vfs := f.make(t)
			ctxS1 := storage.WithSession(context.Background(), "s1")
			ctxS2 := storage.WithSession(context.Background(), "s2")
			ctxTB := storage.WithTenant(storage.WithSession(context.Background(), "s1"), "tenant-b")

			if err := vfs.Write(ctxS1, "secret.txt", []byte("s1 data")); err != nil {
				t.Fatalf("write s1: %v", err)
			}
			if err := vfs.Write(ctxTB, "secret.txt", []byte("tenant-b data")); err != nil {
				t.Fatalf("write tenant-b: %v", err)
			}

			// Another session under the same tenant sees nothing.
			if _, err := vfs.Read(ctxS2, "secret.txt"); !errors.Is(err, storage.ErrFileNotFound) {
				t.Errorf("s2 read = %v, want ErrFileNotFound (no cross-session access)", err)
			}
			if files, _ := vfs.List(ctxS2, ""); len(files) != 0 {
				t.Errorf("s2 list = %+v, want empty", files)
			}

			// Same session id under a different tenant is fully isolated.
			tb, err := vfs.Read(ctxTB, "secret.txt")
			if err != nil || string(tb) != "tenant-b data" {
				t.Errorf("tenant-b read = %q, %v; want 'tenant-b data'", tb, err)
			}
			s1, _ := vfs.Read(ctxS1, "secret.txt")
			if string(s1) != "s1 data" {
				t.Errorf("s1 read = %q, want 's1 data' (tenant-b must not overwrite it)", s1)
			}
		})
	}
}

// Both VFS implementations reject a sessionless context identically.
func TestVFS_SessionlessRejected(t *testing.T) {
	store := newDurableStore(t)
	storageVFS, err := NewStorageVFS(store)
	if err != nil {
		t.Fatalf("NewStorageVFS: %v", err)
	}
	impls := map[string]VFS{"in_memory": NewInMemoryVFS(), "storage": storageVFS}
	ctx := context.Background() // no session
	for name, vfs := range impls {
		t.Run(name, func(t *testing.T) {
			if err := vfs.Write(ctx, "x", []byte("y")); !errors.Is(err, ErrNoSession) {
				t.Errorf("Write err = %v, want ErrNoSession", err)
			}
			if _, err := vfs.Read(ctx, "x"); !errors.Is(err, ErrNoSession) {
				t.Errorf("Read err = %v, want ErrNoSession", err)
			}
			if _, err := vfs.List(ctx, ""); !errors.Is(err, ErrNoSession) {
				t.Errorf("List err = %v, want ErrNoSession", err)
			}
			if err := vfs.Delete(ctx, "x"); !errors.Is(err, ErrNoSession) {
				t.Errorf("Delete err = %v, want ErrNoSession", err)
			}
		})
	}
}

// A path made only of whitespace is rejected.
func TestVFS_RejectsBlankPath(t *testing.T) {
	vfs := NewInMemoryVFS()
	ctx := storage.WithSession(context.Background(), "s")
	if err := vfs.Write(ctx, "   ", []byte("x")); err == nil {
		t.Error("expected error for blank path")
	}
}

// NewStorageVFS fails at construction when the backend lacks SessionFileStore.
func TestNewStorageVFS_UnsupportedBackend(t *testing.T) {
	if _, err := NewStorageVFS(noFilesStore{}); err == nil {
		t.Fatal("expected construction to fail for a backend without SessionFileStore")
	}
}

// noFilesStore is a storage.Storage that does NOT implement SessionFileStore.
type noFilesStore struct{ storage.Storage }

func TestVFSTools_WriteReadListDelete(t *testing.T) {
	vfs := NewInMemoryVFS()
	ctx := storage.WithSession(context.Background(), "sess")

	write := NewFSWriteTool(vfs)
	read := NewFSReadTool(vfs)
	list := NewFSListTool(vfs)
	del := NewFSDeleteTool(vfs)

	// Write returns only path + size, never the content (context stays small).
	res, err := write.Handler(ctx, map[string]any{"path": "out.txt", "content": "hello world"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	m := res.(map[string]any)
	if m["bytes_written"] != 11 {
		t.Errorf("bytes_written = %v, want 11", m["bytes_written"])
	}
	if _, leaked := m["content"]; leaked {
		t.Error("fs_write result must not echo the content back into context")
	}

	// Read pages it back.
	res, err = read.Handler(ctx, map[string]any{"path": "out.txt"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := res.(map[string]any)["content"]; got != "hello world" {
		t.Errorf("read content = %v, want 'hello world'", got)
	}

	// List shows it (metadata only).
	res, _ = list.Handler(ctx, map[string]any{})
	files := res.(map[string]any)["files"].([]map[string]any)
	if len(files) != 1 || files[0]["path"] != "out.txt" {
		t.Errorf("list = %+v, want [out.txt]", files)
	}

	// Delete removes it.
	if _, err := del.Handler(ctx, map[string]any{"path": "out.txt"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := read.Handler(ctx, map[string]any{"path": "out.txt"}); err == nil {
		t.Error("expected read to fail after delete")
	}
}

func TestFSWriteTool_ValidatesArgs(t *testing.T) {
	write := NewFSWriteTool(NewInMemoryVFS())
	ctx := storage.WithSession(context.Background(), "s")

	tests := []struct {
		name string
		args map[string]any
	}{
		{"missing content", map[string]any{"path": "a.txt"}},
		{"non-string content", map[string]any{"path": "a.txt", "content": 42}},
		{"non-string path", map[string]any{"path": 7, "content": "x"}},
		{"blank path", map[string]any{"path": "  ", "content": "x"}},
		{"oversize content", map[string]any{"path": "a.txt", "content": strings.Repeat("x", MaxArtifactBytes+1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := write.Handler(ctx, tt.args); err == nil {
				t.Errorf("expected error for %s", tt.name)
			}
		})
	}

	// A write exactly at the limit succeeds.
	if _, err := write.Handler(ctx, map[string]any{"path": "a.txt", "content": strings.Repeat("x", MaxArtifactBytes)}); err != nil {
		t.Errorf("write at limit should succeed: %v", err)
	}
}

func TestVFSToolkit_RegistersAllTools(t *testing.T) {
	tk := NewVFSToolkit(NewInMemoryVFS())
	got := map[string]bool{}
	for _, n := range tk.ToolNames() {
		got[n] = true
	}
	for _, want := range []string{FSWriteToolName, FSReadToolName, FSListToolName, FSDeleteToolName} {
		if !got[want] {
			t.Errorf("toolkit missing %q (has %v)", want, tk.ToolNames())
		}
	}
}

// Offloading a large artifact keeps the model-visible context tiny: the fs_write
// result is orders of magnitude smaller than the artifact it stored. This is the
// whole point of the VFS — the acceptance "token usage materially lower".
func TestVFS_OffloadingKeepsContextSmall(t *testing.T) {
	vfs := NewInMemoryVFS()
	ctx := storage.WithSession(context.Background(), "sess")
	artifact := strings.Repeat("lorem ipsum dolor sit amet ", 4000) // ~108 KB

	res, err := NewFSWriteTool(vfs).Handler(ctx, map[string]any{"path": "big.txt", "content": artifact})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	// The context cost of the write is the ack, not the artifact.
	ack := fmt.Sprintf("%v", res)
	if len(ack) > 200 {
		t.Errorf("fs_write ack is %d bytes, expected a tiny receipt", len(ack))
	}
	if len(ack)*10 > len(artifact) {
		t.Errorf("offloading did not shrink context: ack=%d artifact=%d", len(ack), len(artifact))
	}
}

// Concurrent writes across sessions must not race. Run with -race.
func TestInMemoryVFS_ConcurrentWrites_NoRace(t *testing.T) {
	vfs := NewInMemoryVFS()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ctx := storage.WithSession(context.Background(), fmt.Sprintf("s%d", i))
			path := fmt.Sprintf("f%d.txt", i)
			if err := vfs.Write(ctx, path, []byte("data")); err != nil {
				t.Errorf("write: %v", err)
				return
			}
			if _, err := vfs.Read(ctx, path); err != nil {
				t.Errorf("read: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

func BenchmarkStorageVFS_WriteRead(b *testing.B) {
	store, err := sqlite.New(filepath.Join(b.TempDir(), "vfs-bench.db"))
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	defer store.Close()
	if err = store.Migrate(context.Background()); err != nil {
		b.Fatalf("migrate: %v", err)
	}
	ctx := storage.WithSession(context.Background(), "bench")
	if err = store.CreateSession(ctx, &storage.Session{ID: "bench", AgentID: "a", Status: "active", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		b.Fatalf("create session: %v", err)
	}
	vfs, err := NewStorageVFS(store)
	if err != nil {
		b.Fatalf("NewStorageVFS: %v", err)
	}
	content := []byte(strings.Repeat("x", 4096))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vfs.Write(ctx, "artifact.bin", content); err != nil {
			b.Fatalf("write: %v", err)
		}
		if _, err := vfs.Read(ctx, "artifact.bin"); err != nil {
			b.Fatalf("read: %v", err)
		}
	}
}
