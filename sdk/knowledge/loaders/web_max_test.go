package loaders

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebLoader_fetchAndExtract_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	l := NewWebLoader([]string{srv.URL}, 0, 0)
	_, err := l.Load()
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 in error, got %v", err)
	}
}

func TestWebLoader_fetchAndExtract_NoText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body><script>x</script></body></html>"))
	}))
	t.Cleanup(srv.Close)

	l := NewWebLoader([]string{srv.URL}, 0, 0)
	_, err := l.Load()
	if err == nil {
		t.Fatal("expected error when no extractable text")
	}
}

func TestWebLoader_Load_SuccessAndChunked(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>Hi</title></head><body><p>Hello world from page.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(srv.Close)

	l := NewWebLoader([]string{srv.URL}, 0, 0)
	docs, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || !strings.Contains(docs[0].Content, "Hello") {
		t.Fatalf("unexpected docs: %+v", docs)
	}

	l2 := NewWebLoader([]string{srv.URL}, 8, 2)
	docs2, err := l2.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(docs2) < 1 {
		t.Fatal("expected chunked docs")
	}
}

func TestWebLoader_WithTimeout_ZeroDuration(t *testing.T) {
	l := NewWebLoader(nil, 0, 0).WithTimeout(0)
	if l.client.Timeout != 0 {
		t.Fatalf("timeout: %v", l.client.Timeout)
	}
}
