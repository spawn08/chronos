package loaders

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebLoader_BasicPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><head><title>Test Page</title></head><body><p>Hello, web world!</p></body></html>`))
	}))
	defer srv.Close()

	loader := NewWebLoader([]string{srv.URL}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	if docs[0].Metadata["title"] != "Test Page" {
		t.Errorf("title = %q, want Test Page", docs[0].Metadata["title"])
	}
	if docs[0].Metadata["type"] != "web" {
		t.Errorf("type = %q, want web", docs[0].Metadata["type"])
	}
}

func TestWebLoader_Chunking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><p>This is a longer page with enough text to be chunked into multiple documents for testing.</p></body></html>`))
	}))
	defer srv.Close()

	loader := NewWebLoader([]string{srv.URL}, 20, 5)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) < 2 {
		t.Fatalf("expected at least 2 chunks, got %d", len(docs))
	}
}

func TestWebLoader_ScriptStripping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><script>var x = 1;</script><p>Visible text</p><style>.hidden{}</style></body></html>`))
	}))
	defer srv.Close()

	loader := NewWebLoader([]string{srv.URL}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := docs[0].Content
	if contains(content, "var x") {
		t.Error("content should not contain script code")
	}
	if !contains(content, "Visible text") {
		t.Error("content should contain visible text")
	}
}

func TestWebLoader_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	loader := NewWebLoader([]string{srv.URL}, 0, 0)
	_, err := loader.Load()
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestWebLoader_MultipleURLs(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>Page one content</body></html>`))
	}))
	defer srv1.Close()
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>Page two content</body></html>`))
	}))
	defer srv2.Close()

	loader := NewWebLoader([]string{srv1.URL, srv2.URL}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestWebLoader_HTMLEntities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><p>Tom &amp; Jerry &mdash; classic</p></body></html>`))
	}))
	defer srv.Close()

	loader := NewWebLoader([]string{srv.URL}, 0, 0)
	docs, err := loader.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(docs[0].Content, "Tom & Jerry") {
		t.Errorf("expected decoded entities, got %q", docs[0].Content)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
