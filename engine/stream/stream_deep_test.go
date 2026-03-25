package stream

import (
	"net/http"
	"testing"
)

// noFlushResponseWriter implements http.ResponseWriter but not http.Flusher.
type noFlushResponseWriter struct {
	header http.Header
	code   int
}

func (n *noFlushResponseWriter) Header() http.Header {
	if n.header == nil {
		n.header = make(http.Header)
	}
	return n.header
}

func (n *noFlushResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (n *noFlushResponseWriter) WriteHeader(statusCode int) {
	n.code = statusCode
}

func TestSSEHandler_NoFlusher_Deep(t *testing.T) {
	b := NewBroker()
	h := b.SSEHandler("sub-1")
	w := &noFlushResponseWriter{}
	r, _ := http.NewRequest(http.MethodGet, "/sse", nil)
	h(w, r)
	if w.code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.code)
	}
}
