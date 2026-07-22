// Example: mcp_sse connects an MCP client to a server over the HTTP+SSE
// transport (MCP 2024-11-05). Unlike the stdio transport, which launches a
// server subprocess, the SSE transport talks to a remote server over HTTP: the
// client opens a long-lived Server-Sent Events stream to receive responses and
// POSTs JSON-RPC requests to an endpoint the server advertises.
//
// To keep the example self-contained and CI-safe, it starts a minimal in-process
// SSE MCP server, then connects to it, lists its tools, and calls one. In a real
// deployment you would point the client at a remote URL instead.
//
//	go run ./examples/mcp_sse/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/spawn08/chronos/engine/mcp"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos MCP over SSE Example                       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// Start the demo SSE MCP server.
	srv := newDemoSSEServer()
	defer srv.Close()
	fmt.Printf("\nSSE server listening at %s\n", srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Build an SSE client. A real client would use a remote URL here.
	client, err := mcp.NewClient(mcp.ServerConfig{
		Name:      "demo-sse",
		Transport: mcp.TransportSSE,
		URL:       srv.URL,
	})
	if err != nil {
		log.Fatalf("new client: %v", err)
	}
	defer client.Close()

	// Connect: open the SSE stream, learn the POST endpoint, and run the
	// initialize handshake.
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("connect: %v", err)
	}
	info := client.Info()
	fmt.Printf("connected to %s v%s (protocol %s)\n", info.Name, info.Version, info.ProtocolVer)

	fmt.Println("\n━━━ Listing tools ━━━")
	tools, err := client.ListTools(ctx)
	if err != nil {
		log.Fatalf("list tools: %v", err)
	}
	for _, t := range tools {
		fmt.Printf("  • %s — %s\n", t.Name, t.Description)
	}

	fmt.Println("\n━━━ Calling the \"echo\" tool ━━━")
	out, err := client.CallTool(ctx, "echo", map[string]any{"text": "hello over SSE"})
	if err != nil {
		log.Fatalf("call tool: %v", err)
	}
	fmt.Printf("  result: %v\n", out)

	fmt.Println("\n✓ MCP over SSE example completed.")
}

// newDemoSSEServer starts an httptest.Server speaking the MCP HTTP+SSE
// transport: the GET stream emits an `endpoint` event pointing at /messages,
// then relays JSON-RPC responses as `message` events; POSTs to /messages are
// answered over the stream.
func newDemoSSEServer() *httptest.Server {
	msgs := make(chan []byte, 16)

	handlers := map[string]func(id int64, params json.RawMessage) any{
		"initialize": func(int64, json.RawMessage) any {
			return map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "demo-sse", "version": "0.1.0"},
			}
		},
		"tools/list": func(int64, json.RawMessage) any {
			return map[string]any{"tools": []map[string]any{
				{"name": "echo", "description": "echoes the provided text back"},
			}}
		},
		"tools/call": func(_ int64, params json.RawMessage) any {
			var p struct {
				Arguments struct {
					Text string `json:"text"`
				} `json:"arguments"`
			}
			_ = json.Unmarshal(params, &p)
			return map[string]any{"content": []map[string]any{
				{"type": "text", "text": "echo: " + p.Arguments.Text},
			}}
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		w.WriteHeader(http.StatusAccepted)

		h, ok := handlers[req.Method]
		if !ok {
			return // notification or unknown method: nothing to stream back
		}
		data, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": req.ID, "result": h(req.ID, req.Params),
		})
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
