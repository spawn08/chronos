package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/tool"
)

// ServeStdio round-trips real newline-delimited JSON-RPC frames: initialize then
// tools/call over an in-memory pipe.
func TestServeStdio_RoundTrip(t *testing.T) {
	s := New("t")
	s.Expose(echoTool())

	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"message":"hola"}}}` + "\n",
	)
	var out strings.Builder
	if err := s.ServeStdio(context.Background(), in, &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}

	// Two requests with ids → exactly two response lines (the notification is silent).
	lines := nonEmptyLines(out.String())
	if len(lines) != 2 {
		t.Fatalf("got %d response lines, want 2:\n%s", len(lines), out.String())
	}
	var initResp rpcResponse
	_ = json.Unmarshal([]byte(lines[0]), &initResp)
	if initResp.Error != nil {
		t.Errorf("initialize error: %+v", initResp.Error)
	}
	if !strings.Contains(lines[1], "hola") {
		t.Errorf("tools/call response missing echoed value: %s", lines[1])
	}
}

func nonEmptyLines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			out = append(out, sc.Text())
		}
	}
	return out
}

// A full conformance round-trip driving the REAL engine/mcp SSE client against
// the server's SSEHandler over httptest: initialize + tools/list + tools/call.
func TestSSE_ConformanceWithRealClient(t *testing.T) {
	s := New("chronos", WithVersion("2.0.0"))
	s.Expose(echoTool())

	ts := httptest.NewServer(s.SSEHandler())
	defer ts.Close()

	client, err := mcp.NewClient(mcp.ServerConfig{
		Name:      "chronos",
		Transport: mcp.TransportSSE,
		URL:       ts.URL,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = client.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	// tools/list surfaces the exposed tool.
	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v, want [echo]", tools)
	}

	// tools/call executes it and returns the result text.
	result, err := client.CallTool(ctx, "echo", map[string]any{"message": "roundtrip"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	text, _ := result.(string)
	if !strings.Contains(text, "roundtrip") {
		t.Errorf("call result = %v, want it to contain the echoed message", result)
	}
}

// The real client observes a denied (require-approval, no approver) tool as an error.
func TestSSE_ApprovalDeniedSurfacesError(t *testing.T) {
	s := New("chronos")
	s.Expose(&tool.Definition{ // no permission → default require-approval, and no approver set
		Name:       "danger",
		Parameters: map[string]any{"type": "object"},
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { return "ran", nil },
	})
	ts := httptest.NewServer(s.SSEHandler())
	defer ts.Close()

	client, _ := mcp.NewClient(mcp.ServerConfig{Name: "c", Transport: mcp.TransportSSE, URL: ts.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	if _, err := client.CallTool(ctx, "danger", map[string]any{}); err == nil {
		t.Fatal("expected an error calling a require-approval tool with no approver")
	}
}
