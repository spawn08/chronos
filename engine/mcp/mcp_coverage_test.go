package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"
)

func TestConnect_StartCommandNotFound(t *testing.T) {
	client, err := NewClient(ServerConfig{Name: "nope", Command: "/nonexistent/mcp/binary/path"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(context.Background()); err == nil {
		t.Fatal("expected start error")
	}
}

func TestConnect_InitializeParseFails(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
		config: ServerConfig{Name: "bad-json"},
	}

	go func() {
		defer serverR.Close()
		defer serverW.Close()
		sc := bufio.NewScanner(serverR)
		if sc.Scan() {
			// Send non-JSON then close stdout so ReadBytes gets EOF
			_, _ = serverW.Write([]byte("not-json-at-all\n"))
		}
	}()

	if err := client.Connect(context.Background()); err == nil {
		t.Fatal("expected initialize parse failure")
	}
	_ = clientW.Close()
	_ = clientR.Close()
}

func TestListResources_Empty(t *testing.T) {
	bin, cleanup := buildMCPEmptyResourcesServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "empty-res", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	res, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("want empty resources, got %d", len(res))
	}
}

func TestListResources_RPCError(t *testing.T) {
	bin, cleanup := buildErrorMCPServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "err", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_, err = client.ListResources(context.Background())
	if err == nil {
		t.Fatal("expected error from resources/list default handler")
	}
}

func TestReadResource_ParseError(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
		config: ServerConfig{Name: "t"},
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
		// Result is a JSON number — cannot unmarshal into struct expecting contents
		_, _ = serverW.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":12345}` + "\n"))
	}()

	_, err = client.ReadResource(context.Background(), "file:///x")
	if err == nil {
		t.Fatal("expected parse error")
	}
	_ = clientW.Close()
	_ = clientR.Close()
}

func TestNotify_WriteError(t *testing.T) {
	w := errWriter{}
	c := &Client{stdin: w}
	if err := c.notify("notifications/initialized", nil); err == nil {
		t.Fatal("expected write error")
	}
}

type errWriter struct{}

func (errWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }

// buildMCPEmptyResourcesServer is like echo server but returns empty resources list.
func buildMCPEmptyResourcesServer(t *testing.T) (string, func()) {
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

func respond(id interface{}, result interface{}) {
	data, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": id, "result": result,
	})
	fmt.Fprintf(os.Stdout, "%s\n", data)
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil { continue }
		if req.ID == nil { continue }
		switch req.Method {
		case "initialize":
			respond(req.ID, map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]interface{}{"name":"empty","version":"1.0.0"},
				"capabilities": map[string]interface{}{},
			})
		case "resources/list":
			respond(req.ID, map[string]interface{}{
				"resources": []interface{}{},
			})
		default:
			respond(req.ID, map[string]interface{}{})
		}
	}
}
`
	tmpDir := t.TempDir()
	srcFile := tmpDir + "/server.go"
	binFile := tmpDir + "/server"
	if err := os.WriteFile(srcFile, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binFile, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return binFile, func() { os.Remove(binFile) }
}
