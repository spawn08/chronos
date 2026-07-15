package mcp

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/tool"
)

// buildHangingMCPServer compiles an MCP server that completes the initialize
// handshake and then never responds to any further request, simulating a hung
// server. It keeps reading stdin so the client's write succeeds and the client
// blocks on the response read.
func buildHangingMCPServer(t *testing.T) (binPath string, cleanup func()) {
	t.Helper()
	src := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string      ` + "`" + `json:"jsonrpc"` + "`" + `
	ID      interface{} ` + "`" + `json:"id"` + "`" + `
	Method  string      ` + "`" + `json:"method"` + "`" + `
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil { continue }
		if req.ID == nil { continue }
		if req.Method == "initialize" {
			data, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{"name":"hang","version":"1.0.0"},
					"capabilities": map[string]interface{}{},
				},
			})
			fmt.Fprintf(os.Stdout, "%s\n", data)
			continue
		}
		// Any other request: intentionally never respond (hang).
	}
}
`
	tmpDir := t.TempDir()
	srcFile := tmpDir + "/hang.go"
	binFile := tmpDir + "/hang"
	if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
		t.Fatalf("write hang server: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binFile, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build hang server: %v: %s", err, out)
	}
	return binFile, func() { os.Remove(binFile) }
}

// connectHangingClient builds and connects a client to a hung MCP server.
func connectHangingClient(t *testing.T) *Client {
	t.Helper()
	bin, cleanup := buildHangingMCPServer(t)
	t.Cleanup(cleanup)

	client, err := NewClient(ServerConfig{Name: "hang", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return client
}

// TestCallContextHonored verifies that a per-call context deadline or
// cancellation returns promptly and releases the request lock, even when the
// server never responds.
func TestCallContextHonored(t *testing.T) {
	tests := []struct {
		name string
		// mkctx returns a context that will expire/cancel and a function to
		// trigger cancellation (may be nil for deadline-based contexts).
		mkctx func() (context.Context, context.CancelFunc)
	}{
		{
			name: "timeout",
			mkctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 150*time.Millisecond)
			},
		},
		{
			name: "cancel",
			mkctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() {
					time.Sleep(150 * time.Millisecond)
					cancel()
				}()
				return ctx, cancel
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := connectHangingClient(t)
			defer client.Close()

			ctx, cancel := tc.mkctx()
			defer cancel()

			done := make(chan error, 1)
			start := time.Now()
			go func() {
				_, err := client.CallTool(ctx, "hang", map[string]any{"x": 1})
				done <- err
			}()

			select {
			case err := <-done:
				if err == nil {
					t.Fatal("expected error from canceled/timed-out call")
				}
				if elapsed := time.Since(start); elapsed > 2*time.Second {
					t.Fatalf("call took too long to honor ctx: %v", elapsed)
				}
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					t.Fatalf("expected ctx error, got: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("call did not return after ctx expired (blocked on read holding lock)")
			}

			// The lock must have been released: a fresh acquisition succeeds
			// immediately. This proves callLocked did not leave mu held.
			if !client.mu.TryLock() {
				t.Fatal("request lock still held after ctx-canceled call")
			}
			client.mu.Unlock()
		})
	}
}

// TestCloseTearsDownHungServer verifies that Close force-kills a hung server
// and unblocks an in-flight call, without deadlocking on the request lock.
func TestCloseTearsDownHungServer(t *testing.T) {
	client := connectHangingClient(t)

	callDone := make(chan error, 1)
	go func() {
		// Background ctx: without Close this would block forever.
		_, err := client.CallTool(context.Background(), "hang", nil)
		callDone <- err
	}()

	// Give the call time to write its request and block on the read.
	time.Sleep(100 * time.Millisecond)

	closeDone := make(chan struct{})
	start := time.Now()
	go func() {
		_ = client.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Fatalf("Close took too long: %v", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close deadlocked on hung server")
	}

	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("expected in-flight call to fail after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight call not unblocked by Close")
	}

	// The subprocess must have been reaped.
	if client.cmd == nil || client.cmd.ProcessState == nil {
		t.Fatal("expected subprocess to be waited/reaped after Close")
	}
}

// TestReadMessageBounded verifies that readMessage enforces the size limit and
// otherwise returns newline-delimited messages.
func TestReadMessageBounded(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		limit   int
		want    string
		wantErr bool
	}{
		{name: "simple line", input: "hello\nworld\n", limit: 1024, want: "hello"},
		{name: "trailing no newline", input: "tail", limit: 1024, want: "tail"},
		{name: "exceeds limit", input: strings.Repeat("a", 100) + "\n", limit: 16, wantErr: true},
		{name: "at limit ok", input: strings.Repeat("a", 8) + "\n", limit: 16, want: strings.Repeat("a", 8)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bufio.NewReader(strings.NewReader(tc.input))
			got, err := readMessage(r, tc.limit)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for oversized message")
				}
				if !strings.Contains(err.Error(), "byte limit") {
					t.Fatalf("expected limit error, got: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readMessage: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNewClientTransportValidation verifies transport handling at construction.
func TestNewClientTransportValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ServerConfig
		wantErr string
	}{
		{name: "stdio ok", cfg: ServerConfig{Name: "a", Command: "echo"}},
		{name: "default stdio ok", cfg: ServerConfig{Name: "a", Command: "echo", Transport: ""}},
		{name: "stdio missing command", cfg: ServerConfig{Name: "a"}, wantErr: "command is required"},
		{name: "sse rejected", cfg: ServerConfig{Name: "a", Transport: TransportSSE, URL: "http://x"}, wantErr: "SSE transport is not implemented"},
		{name: "unknown rejected", cfg: ServerConfig{Name: "a", Transport: Transport("weird")}, wantErr: "unknown transport"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(tc.cfg)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// TestMCPToolsDefaultPermission verifies MCP-registered tools default to
// requiring approval.
func TestMCPToolsDefaultPermission(t *testing.T) {
	infos := []ToolInfo{
		{Name: "danger", Description: "does something", InputSchema: map[string]any{"type": "object"}},
		{Name: "danger2"},
	}

	t.Run("ToolInfoToDefinitions", func(t *testing.T) {
		defs := ToolInfoToDefinitions(&Client{config: ServerConfig{Name: "x"}}, infos)
		if len(defs) != len(infos) {
			t.Fatalf("got %d defs, want %d", len(defs), len(infos))
		}
		for _, d := range defs {
			if d.Permission != tool.PermRequireApproval {
				t.Errorf("tool %q Permission = %q, want %q", d.Name, d.Permission, tool.PermRequireApproval)
			}
		}
	})

	t.Run("RegisterTools", func(t *testing.T) {
		clientR, serverW, _ := os.Pipe()
		serverR, clientW, _ := os.Pipe()
		defer func() {
			serverR.Close()
			serverW.Close()
			clientR.Close()
			clientW.Close()
		}()

		client := &Client{
			stdin:  clientW,
			stdout: bufio.NewReader(clientR),
			config: ServerConfig{Name: "perm-test"},
		}

		go func() {
			sc := bufio.NewScanner(serverR)
			if sc.Scan() {
				_, _ = serverW.WriteString(`{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"danger","description":"d"}]}}` + "\n")
			}
		}()

		reg := tool.NewRegistry()
		n, err := RegisterTools(context.Background(), client, reg)
		if err != nil {
			t.Fatalf("RegisterTools: %v", err)
		}
		if n != 1 {
			t.Fatalf("registered %d tools, want 1", n)
		}
		def, ok := reg.Get("danger")
		if !ok {
			t.Fatal("tool 'danger' not registered")
		}
		if def.Permission != tool.PermRequireApproval {
			t.Errorf("Permission = %q, want %q", def.Permission, tool.PermRequireApproval)
		}
	})
}
