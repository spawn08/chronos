package loaders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLoader_Array(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.json")
	os.WriteFile(f, []byte(`[{"name":"Alice"},{"name":"Bob"}]`), 0644)

	loader := NewJSONLoader([]string{f})
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestJSONLoader_Object(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "data.json")
	os.WriteFile(f, []byte(`{"key":"value"}`), 0644)

	loader := NewJSONLoader([]string{f})
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
}

func TestJSONLoader_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.json")
	os.WriteFile(f, []byte(`{invalid`), 0644)

	loader := NewJSONLoader([]string{f})
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONLoader_MissingFile(t *testing.T) {
	loader := NewJSONLoader([]string{"/nonexistent.json"})
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
