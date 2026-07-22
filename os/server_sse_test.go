package chronosos

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spawn08/chronos/storage/adapters/memory"
)

// TestSSEStream_SurvivesMiddlewareChain is a regression guard. The
// default request logger wraps the ResponseWriter in a statusRecorder; before
// statusRecorder exposed Flush, that wrapper hid the underlying http.Flusher, so
// SSEHandler's `w.(http.Flusher)` check failed and the event stream returned
// 500 "streaming unsupported". This drives the handler through s.Handler() (the
// full chain) rather than s.mux, so the wrapper is actually exercised — the
// existing SSE tests bypass it via s.mux and miss the regression.
func TestSSEStream_SurvivesMiddlewareChain(t *testing.T) {
	s := New(":0", memory.New())

	// Cancel the request context up front so the SSE handler returns promptly
	// after writing its stream headers instead of blocking on the event loop.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events/stream", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()

	s.Handler().ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Fatalf("SSE stream returned 500 through the middleware chain: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "streaming unsupported") {
		t.Fatalf("SSE stream reported streaming unsupported: %s", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}
