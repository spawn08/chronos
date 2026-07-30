// Package mcpserver exposes Chronos tools to any Model Context Protocol (MCP)
// host — Claude Desktop, IDEs, and other agent frameworks — over JSON-RPC 2.0.
// It is the server counterpart to the MCP client in engine/mcp: where the client
// consumes external MCP servers, this lets Chronos BE one.
//
// Tools are sourced from an internal tool.Registry, so a tools/call dispatches
// through Registry.Execute and automatically honors each tool's Permission, the
// approval hook, and the panic-to-error recovery already built into the engine.
// Remote exposure defaults to requiring approval (see WithDefaultPermission),
// following the safe-by-default precedent for remotely invokable tools.
//
// Two transports are provided, mirroring the client: newline-delimited JSON-RPC
// over stdio (ServeStdio) and HTTP+SSE (SSEHandler). The JSON-RPC dispatch is
// transport-agnostic (HandleMessage), so both transports share one code path.
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/tool"
)

// protocolVersion is the MCP revision this server implements, matching the
// engine/mcp client.
const protocolVersion = "2024-11-05"

// Server exposes a set of tools over MCP. It is safe for concurrent use once
// constructed; expose tools and set the approver before serving.
type Server struct {
	name        string
	version     string
	tools       *tool.Registry
	defaultPerm tool.Permission
}

// Option configures a Server.
type Option func(*Server)

// WithVersion sets the server version advertised to clients.
func WithVersion(v string) Option {
	return func(s *Server) { s.version = v }
}

// WithDefaultPermission sets the permission applied to an exposed tool whose
// Definition leaves Permission unset. The default is tool.PermRequireApproval,
// so a remotely invocable tool needs explicit human approval unless the caller
// deliberately exposes it as tool.PermAllow.
func WithDefaultPermission(p tool.Permission) Option {
	return func(s *Server) { s.defaultPerm = p }
}

// New creates an MCP server named name with no tools exposed yet.
func New(name string, opts ...Option) *Server {
	s := &Server{
		name:        name,
		version:     "1.0.0",
		tools:       tool.NewRegistry(),
		defaultPerm: tool.PermRequireApproval,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Expose makes def callable by MCP hosts. A def with no Permission set inherits
// the server's default permission (require-approval), so nothing is silently
// remotely-executable without an explicit opt-in.
//
// It registers a shallow copy rather than mutating the caller's Definition, so
// exposing a tool never changes how it behaves for local (non-MCP) use.
func (s *Server) Expose(def *tool.Definition) {
	exposed := *def
	if exposed.Permission == "" {
		exposed.Permission = s.defaultPerm
	}
	s.tools.Register(&exposed)
}

// ExposeAll exposes every tool in r, applying the default permission to any tool
// that has none. It is a convenience for publishing an existing registry.
func (s *Server) ExposeAll(r *tool.Registry) {
	for _, def := range r.List() {
		s.Expose(def)
	}
}

// SetApprover wires a human-in-the-loop approval service into the tool path, so
// require-approval tools block for a decision before executing. Without one, a
// require-approval tool call fails (never silently runs).
func (s *Server) SetApprover(a tool.Approver) {
	s.tools.SetApprover(a)
}

// --- JSON-RPC 2.0 types ---

type rpcRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"` // absent => notification
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	// ID has no omitempty: a nil json.RawMessage marshals to `null`, which is the
	// JSON-RPC 2.0-mandated id for a response to a parse/invalid-request error
	// whose id could not be determined. Every valid response carries a real id.
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC error codes (subset) per the spec.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInternalError  = -32603
)

// HandleMessage processes one raw JSON-RPC message and returns the raw response
// to send back, or reply=false when the message is a notification (no id) that
// needs no response. It never returns an error: a malformed (but size-bounded)
// message becomes a JSON-RPC error response, and tool failures become isError
// tool results, so one bad message never tears down the transport. Transport-
// level faults (e.g. an over-cap frame) are handled by the transport, not here.
func (s *Server) HandleMessage(ctx context.Context, raw []byte) (response []byte, reply bool) {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return marshalError(nil, codeParseError, "parse error"), true
	}
	// A message with no id is a notification: it never gets a response, even if
	// it is otherwise malformed (JSON-RPC 2.0 §4.1).
	if req.ID == nil {
		return nil, false
	}
	id := *req.ID
	if req.JSONRPC != "2.0" || req.Method == "" {
		return marshalError(id, codeInvalidRequest, "invalid request"), true
	}

	result, rpcErr := s.dispatch(ctx, req)
	if rpcErr != nil {
		return marshalError(id, rpcErr.Code, rpcErr.Message), true
	}
	out, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
	if err != nil {
		return marshalError(id, codeInternalError, "marshal result"), true
	}
	return out, true
}

// dispatch routes a request method to its handler, returning either a result or
// a JSON-RPC error.
func (s *Server) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return s.handleToolsList(), nil
	case "tools/call":
		return s.handleToolsCall(ctx, req.Params)
	default:
		return nil, &rpcError{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

func (s *Server) handleInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": s.name, "version": s.version},
	}
}

// toolInfo is the tools/list entry shape expected by the engine/mcp client.
type toolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

func (s *Server) handleToolsList() map[string]any {
	defs := s.tools.List()
	tools := make([]toolInfo, 0, len(defs))
	for _, def := range defs {
		if def.Permission == tool.PermDeny {
			continue // never advertise a denied tool
		}
		tools = append(tools, toolInfo{
			Name:        def.Name,
			Description: def.Description,
			InputSchema: def.Parameters,
		})
	}
	return map[string]any{"tools": tools}
}

// handleToolsCall dispatches a tool invocation through the registry (which
// enforces permission, approval, and panic recovery) and shapes the outcome as
// an MCP tool result. A tool failure is returned as an isError result — not a
// JSON-RPC error — so the host sees a normal tool response it can show the model.
func (s *Server) handleToolsCall(ctx context.Context, params json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "invalid tools/call params"}
	}
	if p.Name == "" {
		return nil, &rpcError{Code: codeInvalidRequest, Message: "tools/call requires a tool name"}
	}

	result, err := s.tools.Execute(ctx, p.Name, p.Arguments)
	if err != nil {
		return toolResult(err.Error(), true), nil
	}
	return toolResult(resultText(result), false), nil
}

// toolResult builds an MCP tool-call result with a single text content block.
func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

// resultText renders a tool's return value as text: strings pass through,
// everything else is JSON-encoded.
func resultText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// marshalError builds a raw JSON-RPC error response. A nil id marshals to
// `"id":null`, as required when the request id can't be determined.
func marshalError(id json.RawMessage, code int, message string) []byte {
	out, err := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}})
	if err != nil {
		// Fall back to a minimal, always-valid error frame.
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
	}
	return out
}
