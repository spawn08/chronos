package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sseHandler builds a JSON-RPC result for a given request.
type sseHandler func(id int64, params json.RawMessage) any

// newSSETestServer starts an httptest.Server that speaks the MCP 2024-11-05
// HTTP+SSE transport. The GET stream first emits an `endpoint` event pointing
// at /messages, then relays JSON-RPC responses as `message` events. POSTs to
// /messages are dispatched to the supplied handlers; a method with no handler
// is silently dropped (no response is streamed), which lets tests exercise
// timeout and cancellation behavior.
func newSSETestServer(handlers map[string]sseHandler) *httptest.Server {
	msgs := make(chan []byte, 16)

	mux := http.NewServeMux()

	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)

		// Acknowledge the POST; the real JSON-RPC reply arrives over the stream.
		w.WriteHeader(http.StatusAccepted)

		h, ok := handlers[req.Method]
		if !ok {
			return // drop: no response streamed
		}
		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  h(req.ID, req.Params),
		}
		data, _ := json.Marshal(resp)
		select {
		case msgs <- data:
		case <-r.Context().Done():
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case m := <-msgs:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", m)
				flusher.Flush()
			}
		}
	})

	return httptest.NewServer(mux)
}

// defaultSSEHandlers responds to the standard handshake plus tools/list and
// tools/call.
func defaultSSEHandlers() map[string]sseHandler {
	return map[string]sseHandler{
		"initialize": func(id int64, _ json.RawMessage) any {
			return map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo": map[string]any{
					"name":    "test-sse",
					"version": "0.1.0",
				},
			}
		},
		"tools/list": func(id int64, _ json.RawMessage) any {
			return map[string]any{
				"tools": []map[string]any{
					{"name": "echo", "description": "echoes input"},
				},
			}
		},
		"tools/call": func(id int64, _ json.RawMessage) any {
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "hello-from-sse"},
				},
			}
		},
	}
}

func TestSSE_EndToEnd(t *testing.T) {
	srv := newSSETestServer(defaultSSEHandlers())
	defer srv.Close()

	client, err := NewClient(ServerConfig{Name: "sse", Transport: TransportSSE, URL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if info := client.Info(); info.Name != "test-sse" || info.ProtocolVer != "2024-11-05" {
		t.Fatalf("Info = %+v, want name=test-sse protocol=2024-11-05", info)
	}

	t.Run("ListTools", func(t *testing.T) {
		tools, err := client.ListTools(ctx)
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		if len(tools) != 1 || tools[0].Name != "echo" {
			t.Fatalf("tools = %+v, want single echo tool", tools)
		}
	})

	t.Run("CallTool", func(t *testing.T) {
		out, err := client.CallTool(ctx, "echo", map[string]any{"msg": "hi"})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if s, ok := out.(string); !ok || s != "hello-from-sse" {
			t.Fatalf("CallTool = %#v, want %q", out, "hello-from-sse")
		}
	})
}

// TestSSE_ConnectEndpointTimeout verifies Connect honors ctx while waiting for
// the endpoint event: a server that never emits it must not hang Connect.
func TestSSE_ConnectEndpointTimeout(t *testing.T) {
	// A bare server that streams nothing (never sends the endpoint event).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	client, err := NewClient(ServerConfig{Name: "sse", Transport: TransportSSE, URL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	if err := client.Connect(ctx); err == nil {
		t.Fatal("expected Connect to fail waiting for endpoint")
	}
}

// TestSSE_CallTimeout verifies a per-call ctx deadline unblocks a request whose
// response the server never streams.
func TestSSE_CallTimeout(t *testing.T) {
	// Handle only initialize; tools/list is dropped so the call must time out.
	handlers := map[string]sseHandler{
		"initialize": defaultSSEHandlers()["initialize"],
	}
	srv := newSSETestServer(handlers)
	defer srv.Close()

	client, err := NewClient(ServerConfig{Name: "sse", Transport: TransportSSE, URL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	connectCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if cerr := client.Connect(connectCtx); cerr != nil {
		t.Fatalf("Connect: %v", cerr)
	}
	defer client.Close()

	callCtx, callCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer callCancel()

	start := time.Now()
	_, err = client.ListTools(callCtx)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("call took %v, expected to unblock near the 100ms deadline", elapsed)
	}
}

// TestSSE_CloseUnblocksWaiters verifies Close unblocks an in-flight call and
// is idempotent.
func TestSSE_CloseUnblocksWaiters(t *testing.T) {
	handlers := map[string]sseHandler{
		"initialize": defaultSSEHandlers()["initialize"],
	}
	srv := newSSETestServer(handlers)
	defer srv.Close()

	client, err := NewClient(ServerConfig{Name: "sse", Transport: TransportSSE, URL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		// The server never streams a tools/list response; only Close unblocks it.
		_, callErr := client.ListTools(context.Background())
		errCh <- callErr
	}()

	// Give the call time to register and POST before closing.
	time.Sleep(100 * time.Millisecond)

	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent.
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected in-flight call to fail after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not unblock in-flight call")
	}
}
