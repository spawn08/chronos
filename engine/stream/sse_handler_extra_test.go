package stream

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// minimalResponseWriter implements http.ResponseWriter without http.Flusher.
type minimalResponseWriter struct {
	header http.Header
	code   int
	buf    bytes.Buffer
}

func (m *minimalResponseWriter) Header() http.Header {
	if m.header == nil {
		m.header = make(http.Header)
	}
	return m.header
}

func (m *minimalResponseWriter) Write(p []byte) (int, error) {
	return m.buf.Write(p)
}

func (m *minimalResponseWriter) WriteHeader(statusCode int) {
	m.code = statusCode
}

func TestBroker_SSEHandler_RequiresFlusher(t *testing.T) {
	b := NewBroker()
	h := b.SSEHandler("sub1")
	req := httptest.NewRequest(http.MethodGet, "/sse", nil)
	var w minimalResponseWriter
	h.ServeHTTP(&w, req)
	if w.code != http.StatusInternalServerError {
		t.Errorf("code = %d, want 500", w.code)
	}
}
