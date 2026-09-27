package model

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stallingServer writes prefix, then sends nothing until the client leaves.
func stallingServer(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, prefix)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPostStream_IdleTimeoutFailsStalledBody(t *testing.T) {
	srv := stallingServer(t, "data: first\n\n")
	h := newHTTPClient(srv.URL, 5, nil)
	h.streamIdleTimeout = 100 * time.Millisecond

	resp, err := h.postStream(t.Context(), "/", map[string]string{})
	if err != nil {
		t.Fatalf("postStream: %v", err)
	}
	defer resp.Body.Close()

	start := time.Now()
	data, err := io.ReadAll(resp.Body)
	if !errors.Is(err, ErrStreamIdle) {
		t.Fatalf("ReadAll err = %v, want ErrStreamIdle", err)
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("err %v is not a net.Error timeout", err)
	}
	if string(data) != "data: first\n\n" {
		t.Fatalf("data = %q, want the bytes sent before the stall", data)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("idle timeout took %s", elapsed)
	}
}

func TestPostStream_IdleTimeoutResetsOnData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 6; i++ {
			fmt.Fprintf(w, "data: %d\n\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer srv.Close()
	h := newHTTPClient(srv.URL, 5, nil)
	// Total stream time (~240ms) exceeds the timeout; no single gap does.
	h.streamIdleTimeout = 150 * time.Millisecond

	resp, err := h.postStream(t.Context(), "/", map[string]string{})
	if err != nil {
		t.Fatalf("postStream: %v", err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
}

func TestAnthropic_StreamChat_IdleTimeoutSurfacesError(t *testing.T) {
	srv := stallingServer(t, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\n")
	p := NewAnthropicWithConfig(ProviderConfig{APIKey: "test", BaseURL: srv.URL, Model: "claude-opus-4-8"})
	p.http.streamIdleTimeout = 100 * time.Millisecond

	ch, err := p.StreamChat(t.Context(), &ChatRequest{Messages: []Message{{Role: RoleUser, Content: "Hi"}}})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var streamErr error
	for chunk := range ch {
		if chunk != nil && chunk.Err != nil {
			streamErr = chunk.Err
		}
	}
	if !errors.Is(streamErr, ErrStreamIdle) {
		t.Fatalf("stream error = %v, want ErrStreamIdle", streamErr)
	}
}
