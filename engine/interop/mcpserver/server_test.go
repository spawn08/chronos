package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/tool"
)

// call sends one JSON-RPC request through HandleMessage and decodes the response.
func call(t *testing.T, s *Server, id int, method string, params any) rpcResponse {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	raw, _ := json.Marshal(req)
	out, reply := s.HandleMessage(context.Background(), raw)
	if !reply {
		t.Fatalf("%s: expected a reply", method)
	}
	var resp rpcResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("%s: decode response: %v", method, err)
	}
	return resp
}

// resultMap decodes a response's result into a map.
func resultMap(t *testing.T, resp rpcResponse) map[string]any {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return m
}

func echoTool() *tool.Definition {
	return &tool.Definition{
		Name:        "echo",
		Description: "Echo the message back.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"message": map[string]any{"type": "string"}},
			"required":   []any{"message"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			return map[string]any{"echoed": args["message"]}, nil
		},
	}
}

func TestServer_Initialize(t *testing.T) {
	s := New("chronos-test", WithVersion("9.9.9"))
	m := resultMap(t, call(t, s, 1, "initialize", map[string]any{"protocolVersion": protocolVersion}))
	if m["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v, want %s", m["protocolVersion"], protocolVersion)
	}
	info := m["serverInfo"].(map[string]any)
	if info["name"] != "chronos-test" || info["version"] != "9.9.9" {
		t.Errorf("serverInfo = %v", info)
	}
	if _, ok := m["capabilities"].(map[string]any)["tools"]; !ok {
		t.Error("capabilities.tools missing")
	}
}

func TestServer_ToolsList(t *testing.T) {
	s := New("t")
	s.Expose(echoTool())
	// A denied tool must never be advertised.
	s.Expose(&tool.Definition{Name: "secret", Permission: tool.PermDeny, Parameters: map[string]any{}, Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }})

	m := resultMap(t, call(t, s, 1, "tools/list", nil))
	tools := m["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("advertised %d tools, want 1 (denied tool hidden)", len(tools))
	}
	tl := tools[0].(map[string]any)
	if tl["name"] != "echo" || tl["description"] == "" || tl["inputSchema"] == nil {
		t.Errorf("tool entry = %v, want name+description+inputSchema", tl)
	}
}

func TestServer_ToolsCall(t *testing.T) {
	s := New("t")
	s.Expose(echoTool())
	m := resultMap(t, call(t, s, 1, "tools/call", map[string]any{
		"name": "echo", "arguments": map[string]any{"message": "hi"},
	}))
	if m["isError"] != false {
		t.Errorf("isError = %v, want false", m["isError"])
	}
	content := m["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "hi") {
		t.Errorf("content text = %q, want it to contain the echoed message", text)
	}
}

func TestServer_ToolsCall_UnknownToolIsErrorResult(t *testing.T) {
	s := New("t")
	m := resultMap(t, call(t, s, 1, "tools/call", map[string]any{"name": "nope", "arguments": map[string]any{}}))
	if m["isError"] != true {
		t.Errorf("isError = %v, want true for an unknown tool", m["isError"])
	}
}

func TestServer_UnknownMethodIsRPCError(t *testing.T) {
	s := New("t")
	resp := call(t, s, 1, "resources/list", nil)
	if resp.Error == nil || resp.Error.Code != codeMethodNotFound {
		t.Errorf("error = %+v, want method-not-found", resp.Error)
	}
}

func TestServer_Notification_NoReply(t *testing.T) {
	s := New("t")
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if _, reply := s.HandleMessage(context.Background(), raw); reply {
		t.Error("a notification (no id) must not produce a reply")
	}
}

func TestServer_ParseErrorIsRPCError(t *testing.T) {
	s := New("t")
	out, reply := s.HandleMessage(context.Background(), []byte("{not json"))
	if !reply {
		t.Fatal("expected an error reply for malformed JSON")
	}
	var resp rpcResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == nil || resp.Error.Code != codeParseError {
		t.Errorf("error = %+v, want parse error", resp.Error)
	}
	// JSON-RPC 2.0 §5: a response whose id can't be determined MUST carry id=null.
	if !strings.Contains(string(out), `"id":null`) {
		t.Errorf("parse-error frame must include \"id\":null, got: %s", out)
	}
}

// A malformed notification (no id) is silent even though it's invalid, while a
// request with a bad envelope gets an invalid-request error carrying its id.
func TestServer_NotificationSilentEvenIfInvalid(t *testing.T) {
	s := New("t")
	// No id and a bad jsonrpc version → still a notification → no reply.
	if _, reply := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"1.0","method":"x"}`)); reply {
		t.Error("a message with no id must never get a reply, even when invalid")
	}
	// Has an id but a bad envelope → invalid-request error carrying that id.
	out, reply := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"1.0","id":7,"method":"x"}`))
	if !reply {
		t.Fatal("a request with an id must get a reply")
	}
	var resp rpcResponse
	_ = json.Unmarshal(out, &resp)
	if resp.Error == nil || resp.Error.Code != codeInvalidRequest {
		t.Errorf("error = %+v, want invalid request", resp.Error)
	}
	if string(resp.ID) != "7" {
		t.Errorf("error frame id = %s, want 7", resp.ID)
	}
}

// approver is a test tool.Approver with a fixed decision.
type approver struct {
	approve bool
	calls   int
}

func (a *approver) RequestApproval(_ context.Context, _ string, _ map[string]any) (bool, error) {
	a.calls++
	return a.approve, nil
}

func TestServer_ApprovalGate(t *testing.T) {
	tests := []struct {
		name        string
		approve     bool
		wantIsError bool
	}{
		{"approved runs", true, false},
		{"denied blocked", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New("t")
			// Exposed with no explicit permission → default require-approval.
			ran := false
			s.Expose(&tool.Definition{
				Name:       "danger",
				Parameters: map[string]any{"type": "object"},
				Handler:    func(_ context.Context, _ map[string]any) (any, error) { ran = true; return "done", nil },
			})
			app := &approver{approve: tt.approve}
			s.SetApprover(app)

			m := resultMap(t, call(t, s, 1, "tools/call", map[string]any{"name": "danger", "arguments": map[string]any{}}))
			if m["isError"] != tt.wantIsError {
				t.Errorf("isError = %v, want %v", m["isError"], tt.wantIsError)
			}
			if app.calls != 1 {
				t.Errorf("approver called %d times, want 1", app.calls)
			}
			if ran != tt.approve {
				t.Errorf("handler ran = %v, want %v (must run only when approved)", ran, tt.approve)
			}
		})
	}
}

func TestServer_DefaultPermissionApplied(t *testing.T) {
	s := New("t") // default require-approval
	def := &tool.Definition{Name: "x", Parameters: map[string]any{}, Handler: func(_ context.Context, _ map[string]any) (any, error) { return nil, nil }}
	s.Expose(def)

	got, _ := s.tools.Get("x")
	if got.Permission != tool.PermRequireApproval {
		t.Errorf("exposed permission = %q, want require_approval by default", got.Permission)
	}
	// Exposing must not mutate the caller's Definition (no surprising side effect
	// on the tool's local behavior).
	if def.Permission != "" {
		t.Errorf("caller's def.Permission was mutated to %q; Expose must copy", def.Permission)
	}
}

// A panicking tool fails only its own call (recover is in the registry) — the
// server returns an isError result rather than crashing.
func TestServer_PanickingToolIsContained(t *testing.T) {
	s := New("t")
	s.Expose(&tool.Definition{
		Name:       "boom",
		Permission: tool.PermAllow,
		Parameters: map[string]any{"type": "object"},
		Handler:    func(_ context.Context, _ map[string]any) (any, error) { panic("kaboom") },
	})
	m := resultMap(t, call(t, s, 1, "tools/call", map[string]any{"name": "boom", "arguments": map[string]any{}}))
	if m["isError"] != true {
		t.Errorf("isError = %v, want true for a panicking tool", m["isError"])
	}
	// The server is still usable afterward.
	if resp := call(t, s, 2, "ping", nil); resp.Error != nil {
		t.Errorf("server unusable after a panicking tool: %+v", resp.Error)
	}
}

func TestResultText(t *testing.T) {
	if got := resultText("plain"); got != "plain" {
		t.Errorf("string passthrough = %q", got)
	}
	if got := resultText(map[string]any{"a": 1}); got != `{"a":1}` {
		t.Errorf("json encode = %q", got)
	}
}
