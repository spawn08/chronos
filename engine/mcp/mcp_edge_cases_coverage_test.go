package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestReadResource_RPCError(t *testing.T) {
	bin, cleanup := buildErrorMCPServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "read-res-err", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err = client.ReadResource(context.Background(), "file:///any")
	if err == nil {
		t.Fatal("expected resources/read RPC error")
	}
	if !strings.Contains(err.Error(), "resources/read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListResources_ParseError(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	c := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
		config: ServerConfig{Name: "parse-list"},
	}

	go func() {
		defer serverR.Close()
		defer serverW.Close()
		sc := bufio.NewScanner(serverR)
		if !sc.Scan() {
			return
		}
		var req jsonrpcRequest
		_ = json.Unmarshal(sc.Bytes(), &req)
		_, _ = serverW.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"not-an-object"}` + "\n"))
	}()

	_, err = c.ListResources(context.Background())
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse resources") {
		t.Fatalf("unexpected: %v", err)
	}
	_ = clientW.Close()
	_ = clientR.Close()
}

func TestNotify_AfterClientClose(t *testing.T) {
	bin, cleanup := buildMCPEmptyResourcesServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "notify-after-close", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = client.Close()

	if err := client.notify("notifications/initialized", nil); err == nil {
		t.Fatal("expected notify error after close")
	}
}

// errWriter is defined in mcp_coverage_test.go; Go requires io.WriteCloser for Client.stdin in struct literals.
func (errWriter) Close() error { return nil }
