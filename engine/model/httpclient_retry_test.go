package model

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// noopSleep records the requested delays without actually sleeping.
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (s *sleepRecorder) fn(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.delays = append(s.delays, d)
	s.mu.Unlock()
	return ctx.Err()
}

func TestHTTPClient_TransportTuning(t *testing.T) {
	h := newHTTPClient("http://x", 30, nil)
	if h.transport.MaxIdleConns != defaultMaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", h.transport.MaxIdleConns, defaultMaxIdleConns)
	}
	if h.transport.MaxIdleConnsPerHost != defaultMaxIdleConnsPerHost {
		t.Errorf("MaxIdleConnsPerHost = %d, want %d", h.transport.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost)
	}
	if h.transport.IdleConnTimeout != defaultIdleConnTimeout {
		t.Errorf("IdleConnTimeout = %v", h.transport.IdleConnTimeout)
	}
	if h.transport.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 30s", h.transport.ResponseHeaderTimeout)
	}
	// Unary client is bounded by an overall timeout; the streaming client is not.
	if h.client.Timeout != 30*time.Second {
		t.Errorf("unary Timeout = %v, want 30s", h.client.Timeout)
	}
	if h.streamClient.Timeout != 0 {
		t.Errorf("stream Timeout = %v, want 0 (unbounded)", h.streamClient.Timeout)
	}
	// Both clients share the tuned transport (one connection pool).
	if h.client.Transport != h.streamClient.Transport {
		t.Error("unary and streaming clients should share one transport")
	}
}

func TestHTTPClient_RetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	rec := &sleepRecorder{}
	h := newHTTPClient(srv.URL, 5, nil, withMaxRetries(3), withSleepFn(rec.fn))
	resp, err := h.post(context.Background(), "/x", map[string]string{})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
	if len(rec.delays) != 2 {
		t.Errorf("backoff waits = %d, want 2", len(rec.delays))
	}
}

func TestHTTPClient_RetryHonorsRetryAfter(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	rec := &sleepRecorder{}
	h := newHTTPClient(srv.URL, 5, nil, withMaxRetries(2), withSleepFn(rec.fn))
	resp, err := h.post(context.Background(), "/x", map[string]string{})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer drainAndClose(resp.Body)
	if len(rec.delays) != 1 || rec.delays[0] != 7*time.Second {
		t.Errorf("delays = %v, want [7s] (honoring Retry-After)", rec.delays)
	}
}

func TestHTTPClient_TerminalStatusNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	h := newHTTPClient(srv.URL, 5, nil, withMaxRetries(3), withSleepFn((&sleepRecorder{}).fn))
	resp, err := h.post(context.Background(), "/x", map[string]string{})
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer drainAndClose(resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("server calls = %d, want 1 (4xx must not be retried)", got)
	}
}

func TestHTTPClient_CircuitBreakerOpens(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cb := newCircuitBreaker(2, time.Hour)
	h := newHTTPClient(srv.URL, 5, nil, withCircuitBreaker(cb), withSleepFn((&sleepRecorder{}).fn))
	// maxRetries defaults to 0, so each post is a single attempt → one failure each.
	for i := 0; i < 2; i++ {
		resp, err := h.post(context.Background(), "/x", map[string]string{})
		if err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
		drainAndClose(resp.Body)
	}
	callsBefore := atomic.LoadInt32(&calls)
	// Breaker is now open: the next call short-circuits without hitting the server.
	_, err := h.post(context.Background(), "/x", map[string]string{})
	if err == nil {
		t.Fatal("expected circuit-open error")
	}
	if IsRetryable(err) {
		t.Error("circuit-open error must not be classified as retryable")
	}
	if atomic.LoadInt32(&calls) != callsBefore {
		t.Errorf("server was hit while breaker open: before=%d after=%d", callsBefore, atomic.LoadInt32(&calls))
	}
}

func TestHTTPClient_ContextCancelStopsRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := newHTTPClient(srv.URL, 5, nil, withMaxRetries(5), withSleepFn(func(c context.Context, _ time.Duration) error {
		return c.Err()
	}))
	_, err := h.post(ctx, "/x", map[string]string{})
	if err == nil {
		t.Fatal("expected error when context is canceled")
	}
}
