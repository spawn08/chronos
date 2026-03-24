package loaders

import (
	"os"
	"path/filepath"
	"testing"
)

// minimalPDF creates a minimal valid PDF with extractable text.
func minimalPDF(text string) []byte {
	// Minimal PDF 1.4 with a single text stream
	return []byte(`%PDF-1.4
1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj
2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj
3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R>>endobj
4 0 obj<</Length 44>>stream
BT /F1 12 Tf 100 700 Td (` + text + `) Tj ET
endstream
endobj
xref
0 5
trailer<</Size 5/Root 1 0 R>>
startxref
0
%%EOF`)
}

func TestPDFLoader_SingleFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "test.pdf")
	if err := os.WriteFile(f, minimalPDF("Hello PDF World"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewPDFLoader([]string{f}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Metadata["type"] != "pdf" {
		t.Errorf("type = %q, want pdf", docs[0].Metadata["type"])
	}
	if docs[0].Content == "" {
		t.Error("content should not be empty")
	}
}

func TestPDFLoader_Chunking(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "big.pdf")
	if err := os.WriteFile(f, minimalPDF("This is a longer text for chunking test purposes"), 0644); err != nil {
		t.Fatal(err)
	}

	loader := NewPDFLoader([]string{f}, 10, 2)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(docs))
	}
	if docs[0].Metadata["chunk_idx"] != 0 {
		t.Errorf("first chunk index should be 0")
	}
}

func TestPDFLoader_InvalidPDF(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.pdf")
	os.WriteFile(f, []byte("not a pdf"), 0644)

	loader := NewPDFLoader([]string{f}, 0, 0)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for invalid PDF")
	}
}

func TestPDFLoader_MissingFile(t *testing.T) {
	loader := NewPDFLoader([]string{"/nonexistent/file.pdf"}, 0, 0)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPDFLoader_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.pdf")
	f2 := filepath.Join(dir, "b.pdf")
	os.WriteFile(f1, minimalPDF("First document"), 0644)
	os.WriteFile(f2, minimalPDF("Second document"), 0644)

	loader := NewPDFLoader([]string{f1, f2}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}
