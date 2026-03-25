package loaders

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONLoader_PrimitiveRoot(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "primitive.json")
	if err := os.WriteFile(f, []byte(`42`), 0644); err != nil {
		t.Fatal(err)
	}
	loader := NewJSONLoader([]string{f})
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc for primitive JSON, got %d", len(docs))
	}
	if docs[0].Content != "42" {
		t.Errorf("content = %q", docs[0].Content)
	}
}
