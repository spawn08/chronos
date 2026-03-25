package loaders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCSVLoader_WithHeader(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.csv")
	os.WriteFile(f, []byte("name,age\nAlice,30\nBob,25"), 0o644)

	loader := NewCSVLoader([]string{f}, true)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	if docs[0].Content != "name: Alice, age: 30" {
		t.Errorf("content = %q", docs[0].Content)
	}
}

func TestCSVLoader_NoHeader(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.csv")
	os.WriteFile(f, []byte("Alice,30\nBob,25"), 0o644)

	loader := NewCSVLoader([]string{f}, false)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
	if docs[0].Content != "Alice, 30" {
		t.Errorf("content = %q", docs[0].Content)
	}
}

func TestCSVLoader_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.csv")
	os.WriteFile(f, []byte(""), 0o644)

	loader := NewCSVLoader([]string{f}, true)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 0 {
		t.Errorf("expected 0 docs, got %d", len(docs))
	}
}
