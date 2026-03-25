package loaders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTextLoader_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.txt")
	os.WriteFile(f, []byte("Hello, world!"), 0o644)

	loader := NewTextLoader([]string{f}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Content != "Hello, world!" {
		t.Errorf("content = %q", docs[0].Content)
	}
	if docs[0].Metadata["source"] != f {
		t.Errorf("source = %q", docs[0].Metadata["source"])
	}
}

func TestTextLoader_Chunking(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "big.txt")
	os.WriteFile(f, []byte("ABCDEFGHIJ"), 0o644)

	loader := NewTextLoader([]string{f}, 4, 1)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(docs))
	}
	if docs[0].Content != "ABCD" {
		t.Errorf("chunk 0 = %q, want ABCD", docs[0].Content)
	}
}

func TestTextLoader_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.txt")
	os.WriteFile(f1, []byte("file one"), 0o644)
	os.WriteFile(f2, []byte("file two"), 0o644)

	loader := NewTextLoader([]string{f1, f2}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestTextLoader_MissingFile(t *testing.T) {
	loader := NewTextLoader([]string{"/nonexistent/file.txt"}, 0, 0)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestChunkText(t *testing.T) {
	tests := []struct {
		text    string
		size    int
		overlap int
		want    int
	}{
		{"short", 100, 0, 1},
		{"ABCDEFGHIJ", 3, 0, 4},
		{"ABCDEFGHIJ", 5, 2, 3},
	}
	for _, tt := range tests {
		chunks := chunkText(tt.text, tt.size, tt.overlap)
		if len(chunks) != tt.want {
			t.Errorf("chunkText(%q, %d, %d) = %d chunks, want %d", tt.text, tt.size, tt.overlap, len(chunks), tt.want)
		}
	}
}
