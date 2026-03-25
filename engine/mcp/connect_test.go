package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/tool"
)

// buildMCPEchoServer compiles a minimal MCP server binary for use in tests.
func buildMCPEchoServer(t *testing.T) (binPath string, cleanup func()) {
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
				"serverInfo": map[string]interface{}{"name":"echo-server","version":"1.0.0"},
				"capabilities": map[string]interface{}{},
			})
		case "tools/list":
			respond(req.ID, map[string]interface{}{
				"tools": []interface{}{
					map[string]interface{}{
						"name": "echo", "description": "Echoes input",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"message": map[string]interface{}{"type": "string"},
							},
						},
					},
				},
			})
		case "tools/call":
			respond(req.ID, map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": "echoed"},
				},
				"isError": false,
			})
		case "resources/list":
			respond(req.ID, map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"uri": "file:///tmp/test.txt", "name": "test.txt",
						"description": "A test resource", "mimeType": "text/plain",
					},
				},
			})
		case "resources/read":
			respond(req.ID, map[string]interface{}{
				"contents": []interface{}{
					map[string]interface{}{
						"uri": "file:///tmp/test.txt", "mimeType": "text/plain",
						"text": "hello content",
					},
				},
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
		t.Fatalf("write server.go: %v", err)
	}
	cmd := exec.Command("go", "build", "-o", binFile, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build echo server: %v: %s", err, out)
	}
	return binFile, func() { os.Remove(binFile) }
}

func TestClient_ConnectAndListTools(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	info := client.Info()
	if info.Name != "echo-server" {
		t.Errorf("Info.Name = %q, want 'echo-server'", info.Name)
	}
	if info.ProtocolVer != "2024-11-05" {
		t.Errorf("Info.ProtocolVer = %q", info.ProtocolVer)
	}

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("tools[0].Name = %q, want 'echo'", tools[0].Name)
	}
}

func TestClient_CallTool(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	result, err := client.CallTool(context.Background(), "echo", map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "echoed" {
		t.Errorf("CallTool result = %v, want 'echoed'", result)
	}
}

func TestClient_ListResources(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	resources, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].URI != "file:///tmp/test.txt" {
		t.Errorf("resources[0].URI = %q", resources[0].URI)
	}
}

func TestClient_ReadResource(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	contents, err := client.ReadResource(context.Background(), "file:///tmp/test.txt")
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}
	if contents[0].Text != "hello content" {
		t.Errorf("content text = %q, want 'hello content'", contents[0].Text)
	}
}

func TestClient_CloseProcess(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Second Close: %v", err)
	}
}

func TestClient_CallAfterClose(t *testing.T) {
	bin, cleanup := buildMCPEchoServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "echo", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	client.Close()

	_, err = client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error when calling after close")
	}
}

func buildErrorMCPServer(t *testing.T) (binPath string, cleanup func()) {
	t.Helper()
	src := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type req struct {
	JSONRPC string      ` + "`" + `json:"jsonrpc"` + "`" + `
	ID      interface{} ` + "`" + `json:"id"` + "`" + `
	Method  string      ` + "`" + `json:"method"` + "`" + `
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var r req
		json.Unmarshal(scanner.Bytes(), &r)
		if r.ID == nil { continue }
		switch r.Method {
		case "initialize":
			data, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": r.ID,
				"result": map[string]interface{}{
					"protocolVersion": "2024-11-05",
					"serverInfo": map[string]interface{}{"name":"err-server","version":"1.0"},
				},
			})
			fmt.Fprintln(os.Stdout, string(data))
		case "tools/call":
			data, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": r.ID,
				"result": map[string]interface{}{
					"content": []interface{}{
						map[string]interface{}{"type":"text","text":"tool failed"},
					},
					"isError": true,
				},
			})
			fmt.Fprintln(os.Stdout, string(data))
		default:
			data, _ := json.Marshal(map[string]interface{}{
				"jsonrpc": "2.0", "id": r.ID,
				"error": map[string]interface{}{"code": -32601, "message": "method not found"},
			})
			fmt.Fprintln(os.Stdout, string(data))
		}
	}
}
`
	tmpDir := t.TempDir()
	srcFile := tmpDir + "/server_err.go"
	binFile := tmpDir + "/server_err"
	os.WriteFile(srcFile, []byte(src), 0o644)
	cmd := exec.Command("go", "build", "-o", binFile, srcFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build error server: %v: %s", err, out)
	}
	return binFile, func() { os.Remove(binFile) }
}

func TestClient_ToolCallError(t *testing.T) {
	bin, cleanup := buildErrorMCPServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "err", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err = client.CallTool(context.Background(), "fail_tool", nil)
	if err == nil {
		t.Fatal("expected error for tool isError=true")
	}
	if !strings.Contains(err.Error(), "tool failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestClient_ListToolsServerError(t *testing.T) {
	bin, cleanup := buildErrorMCPServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "err", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	_, err = client.ListTools(context.Background())
	if err == nil {
		t.Fatal("expected error from server error response")
	}
}

func buildMultiContentMCPServer(t *testing.T) (binPath string, cleanup func()) {
	t.Helper()
	src := `package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type req struct {
	JSONRPC string      ` + "`" + `json:"jsonrpc"` + "`" + `
	ID      interface{} ` + "`" + `json:"id"` + "`" + `
	Method  string      ` + "`" + `json:"method"` + "`" + `
}

func send(id interface{}, result interface{}) {
	data, _ := json.Marshal(map[string]interface{}{"jsonrpc":"2.0","id":id,"result":result})
	fmt.Fprintln(os.Stdout, string(data))
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var r req
		json.Unmarshal(scanner.Bytes(), &r)
		if r.ID == nil { continue }
		switch r.Method {
		case "initialize":
			send(r.ID, map[string]interface{}{
				"protocolVersion":"2024-11-05",
				"serverInfo":map[string]interface{}{"name":"multi","version":"1.0"},
			})
		case "tools/call":
			send(r.ID, map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type":"text","text":"part1"},
					map[string]interface{}{"type":"text","text":"part2"},
				},
				"isError": false,
			})
		}
	}
}
`
	tmpDir := t.TempDir()
	src2 := tmpDir + "/mc.go"
	bin := tmpDir + "/mc"
	os.WriteFile(src2, []byte(src), 0o644)
	cmd := exec.Command("go", "build", "-o", bin, src2)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build multi-content server: %v: %s", err, out)
	}
	return bin, func() { os.Remove(bin) }
}

func TestClient_CallTool_MultipleContent(t *testing.T) {
	bin, cleanup := buildMultiContentMCPServer(t)
	defer cleanup()

	client, err := NewClient(ServerConfig{Name: "multi", Command: bin})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err = client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	result, err := client.CallTool(context.Background(), "tool", nil)
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	texts, ok := result.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result)
	}
	if len(texts) != 2 {
		t.Fatalf("expected 2 items, got %d", len(texts))
	}
	if texts[0] != "part1" || texts[1] != "part2" {
		t.Errorf("unexpected texts: %v", texts)
	}
}

func TestNotify_WritesCorrectJSON(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	client := &Client{
		stdin:  w,
		stdout: bufio.NewReader(r),
	}

	if err := client.notify("test/method", map[string]any{"key": "val"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	w.Close()

	data := make([]byte, 1024)
	n, _ := r.Read(data)
	r.Close()

	line := strings.TrimSpace(string(data[:n]))
	var msg map[string]any
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		t.Fatalf("unmarshal notify: %v: %s", err, line)
	}
	if msg["method"] != "test/method" {
		t.Errorf("method = %v, want 'test/method'", msg["method"])
	}
	if msg["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want '2.0'", msg["jsonrpc"])
	}
}

func TestCallLocked_ClientClosed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	client := &Client{
		stdin:  w,
		stdout: bufio.NewReader(r),
		closed: true,
	}

	_, err = client.callLocked(context.Background(), "test/method", nil)
	if err == nil {
		t.Fatal("expected error when calling closed client")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("error should mention closed: %v", err)
	}
	w.Close()
	r.Close()
}

func TestCallLocked_ValidRoundTrip(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe1: %v", err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe2: %v", err)
	}

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
	}

	// Simulate a server reading request and writing response in a goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(serverR)
		if !scanner.Scan() {
			return
		}
		var req jsonrpcRequest
		json.Unmarshal(scanner.Bytes(), &req)

		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
		}
		resultData, _ := json.Marshal(map[string]string{"status": "ok"})
		resp.Result = resultData
		data, _ := json.Marshal(resp)
		serverW.Write(append(data, '\n'))
	}()

	result, err := client.callLocked(context.Background(), "test/echo", map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("callLocked: %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", parsed["status"])
	}

	<-done
	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

func TestCallLocked_ServerError(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe1: %v", err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe2: %v", err)
	}

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
	}

	go func() {
		scanner := bufio.NewScanner(serverR)
		if !scanner.Scan() {
			return
		}
		var req jsonrpcRequest
		json.Unmarshal(scanner.Bytes(), &req)

		resp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error": map[string]any{
				"code":    -32600,
				"message": "invalid request",
			},
		}
		data, _ := json.Marshal(resp)
		serverW.Write(append(data, '\n'))
	}()

	_, err = client.callLocked(context.Background(), "bad/method", nil)
	if err == nil {
		t.Fatal("expected error for server error response")
	}
	if !strings.Contains(err.Error(), "invalid request") {
		t.Errorf("error should contain server message: %v", err)
	}

	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

func TestCallLocked_SkipsNonMatchingIDs(t *testing.T) {
	clientR, serverW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe1: %v", err)
	}
	serverR, clientW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe2: %v", err)
	}

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
	}

	go func() {
		scanner := bufio.NewScanner(serverR)
		if !scanner.Scan() {
			return
		}
		var req jsonrpcRequest
		json.Unmarshal(scanner.Bytes(), &req)

		// First: send a notification (no id match)
		notif := map[string]any{"jsonrpc": "2.0", "method": "notification"}
		d1, _ := json.Marshal(notif)
		serverW.Write(append(d1, '\n'))

		// Second: send response with wrong ID
		wrongResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      int64(99999),
			"result":  map[string]string{"wrong": "true"},
		}
		d2, _ := json.Marshal(wrongResp)
		serverW.Write(append(d2, '\n'))

		// Third: send correct response
		correctResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]string{"correct": "true"},
		}
		d3, _ := json.Marshal(correctResp)
		serverW.Write(append(d3, '\n'))
	}()

	result, err := client.callLocked(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("callLocked: %v", err)
	}

	var parsed map[string]string
	json.Unmarshal(result, &parsed)
	if parsed["correct"] != "true" {
		t.Errorf("expected correct response, got %v", parsed)
	}

	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

func TestCallLocked_ReadError(t *testing.T) {
	_, clientW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	// Create a reader that immediately returns EOF
	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(strings.NewReader("")),
	}

	_, err = client.callLocked(context.Background(), "test", nil)
	if err == nil {
		t.Fatal("expected error when reader is exhausted")
	}
	if !strings.Contains(err.Error(), "read") {
		t.Errorf("error should mention read: %v", err)
	}
	clientW.Close()
}

func TestNewClient_EmptyTransport_DefaultsToStdio(t *testing.T) {
	client, err := NewClient(ServerConfig{Name: "t", Command: "cat"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.config.Transport != TransportStdio {
		t.Errorf("transport = %q, want stdio", client.config.Transport)
	}
}

func TestClient_InfoBeforeConnect(t *testing.T) {
	client, _ := NewClient(ServerConfig{Name: "t", Command: "cat"})
	info := client.Info()
	if info.Name != "" || info.Version != "" || info.ProtocolVer != "" {
		t.Error("expected empty ServerInfo before Connect")
	}
}

func TestClient_Call_HoldsLock(t *testing.T) {
	clientR, serverW, _ := os.Pipe()
	serverR, clientW, _ := os.Pipe()

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
	}

	go func() {
		scanner := bufio.NewScanner(serverR)
		if scanner.Scan() {
			var req jsonrpcRequest
			json.Unmarshal(scanner.Bytes(), &req)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "ok",
			}
			data, _ := json.Marshal(resp)
			serverW.Write(append(data, '\n'))
		}
	}()

	result, err := client.call(context.Background(), "test", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if string(result) != `"ok"` {
		t.Errorf("result = %s, want \"ok\"", string(result))
	}

	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

func TestCallTool_ErrorWithNoContent(t *testing.T) {
	clientR, serverW, _ := os.Pipe()
	serverR, clientW, _ := os.Pipe()

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
	}

	go func() {
		scanner := bufio.NewScanner(serverR)
		if scanner.Scan() {
			var req jsonrpcRequest
			json.Unmarshal(scanner.Bytes(), &req)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []any{},
					"isError": true,
				},
			}
			data, _ := json.Marshal(resp)
			serverW.Write(append(data, '\n'))
		}
	}()

	_, err := client.CallTool(context.Background(), "broken", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("expected 'unknown' in error, got: %v", err)
	}

	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

func TestRegisterTools_WithPipedServer(t *testing.T) {
	clientR, serverW, _ := os.Pipe()
	serverR, clientW, _ := os.Pipe()

	client := &Client{
		stdin:  clientW,
		stdout: bufio.NewReader(clientR),
		config: ServerConfig{Name: "pipe-test"},
	}

	go func() {
		scanner := bufio.NewScanner(serverR)
		if scanner.Scan() {
			var req jsonrpcRequest
			json.Unmarshal(scanner.Bytes(), &req)
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []any{
						map[string]any{
							"name":        "calc",
							"description": "Calculator",
							"inputSchema": map[string]any{"type": "object"},
						},
						map[string]any{
							"name":        "search",
							"description": "Search",
						},
					},
				},
			}
			data, _ := json.Marshal(resp)
			serverW.Write(append(data, '\n'))
		}
	}()

	registry := tool.NewRegistry()
	count, err := RegisterTools(context.Background(), client, registry)
	if err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 tools registered, got %d", count)
	}

	serverR.Close()
	serverW.Close()
	clientR.Close()
	clientW.Close()
}

// Suppress unused import warnings
var _ = fmt.Sprintf
