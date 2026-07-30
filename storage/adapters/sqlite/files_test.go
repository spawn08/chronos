package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/storage"
)

func TestSessionFileStore_RoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.PutFile(ctx, "sess", "a/b.txt", []byte("hello")); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := store.GetFile(ctx, "sess", "a/b.txt")
	if err != nil || string(got) != "hello" {
		t.Fatalf("get = %q, %v; want hello", got, err)
	}

	// Overwrite updates content and size.
	if err = store.PutFile(ctx, "sess", "a/b.txt", []byte("hello world")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	files, err := store.ListFiles(ctx, "sess", "a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 1 || files[0].Path != "a/b.txt" || files[0].Size != len("hello world") {
		t.Errorf("list = %+v, want one a/b.txt of size 11", files)
	}

	if err := store.DeleteFile(ctx, "sess", "a/b.txt"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.GetFile(ctx, "sess", "a/b.txt"); !errors.Is(err, storage.ErrFileNotFound) {
		t.Errorf("get after delete = %v, want ErrFileNotFound", err)
	}
	// Delete is idempotent.
	if err := store.DeleteFile(ctx, "sess", "a/b.txt"); err != nil {
		t.Errorf("second delete = %v, want nil (idempotent)", err)
	}
}

func TestSessionFileStore_ListPrefixOrdered(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, p := range []string{"z.txt", "notes/b.md", "notes/a.md", "draft.md"} {
		if err := store.PutFile(ctx, "s", p, []byte("x")); err != nil {
			t.Fatalf("put %q: %v", p, err)
		}
	}
	files, err := store.ListFiles(ctx, "s", "notes/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(files) != 2 || files[0].Path != "notes/a.md" || files[1].Path != "notes/b.md" {
		t.Errorf("prefix list = %+v, want [notes/a.md notes/b.md] ordered", files)
	}
	all, _ := store.ListFiles(ctx, "s", "")
	if len(all) != 4 {
		t.Errorf("list all = %d files, want 4", len(all))
	}
}

// The prefix scan is a byte-wise range, so SQL LIKE metacharacters (% _) are
// matched literally and matching is case-SENSITIVE (SQLite LIKE would fail both).
func TestSessionFileStore_ListPrefixLiteralAndCaseSensitive(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	for _, p := range []string{"a%b.txt", "axb.txt", "a_c.txt", "aZc.txt", "notes/x", "Notes/y"} {
		if err := store.PutFile(ctx, "s", p, []byte("x")); err != nil {
			t.Fatalf("put %q: %v", p, err)
		}
	}
	// '%' is a literal byte, not a wildcard: "a%" matches only "a%b.txt".
	pct, err := store.ListFiles(ctx, "s", "a%")
	if err != nil {
		t.Fatalf("list a%%: %v", err)
	}
	if len(pct) != 1 || pct[0].Path != "a%b.txt" {
		t.Errorf("prefix 'a%%' = %+v, want only [a%%b.txt]", pct)
	}
	// '_' is a literal byte: "a_" matches only "a_c.txt".
	usc, err := store.ListFiles(ctx, "s", "a_")
	if err != nil {
		t.Fatalf("list a_: %v", err)
	}
	if len(usc) != 1 || usc[0].Path != "a_c.txt" {
		t.Errorf("prefix 'a_' = %+v, want only [a_c.txt]", usc)
	}
	// Case-sensitive: "notes/" must NOT match "Notes/y".
	lower, err := store.ListFiles(ctx, "s", "notes/")
	if err != nil {
		t.Fatalf("list notes/: %v", err)
	}
	if len(lower) != 1 || lower[0].Path != "notes/x" {
		t.Errorf("prefix 'notes/' = %+v, want only [notes/x] (case-sensitive)", lower)
	}
}

func TestSessionFileStore_TenantAndSessionIsolation(t *testing.T) {
	store := newTestStore(t)
	base := context.Background()
	ctxA := storage.WithTenant(base, "tenant-a")
	ctxB := storage.WithTenant(base, "tenant-b")

	if err := store.PutFile(ctxA, "sess", "f.txt", []byte("A")); err != nil {
		t.Fatalf("put A: %v", err)
	}
	// Same session id + path under a different tenant is a distinct file.
	if err := store.PutFile(ctxB, "sess", "f.txt", []byte("B")); err != nil {
		t.Fatalf("put B: %v", err)
	}

	a, _ := store.GetFile(ctxA, "sess", "f.txt")
	b, _ := store.GetFile(ctxB, "sess", "f.txt")
	if string(a) != "A" || string(b) != "B" {
		t.Errorf("tenant isolation broken: A=%q B=%q", a, b)
	}

	// A different session under the same tenant sees nothing.
	if _, err := store.GetFile(ctxA, "other", "f.txt"); !errors.Is(err, storage.ErrFileNotFound) {
		t.Errorf("cross-session read = %v, want ErrFileNotFound", err)
	}
}
