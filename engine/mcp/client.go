// Package mcp implements the Model Context Protocol (MCP) client.
// It supports connecting to MCP servers via stdio transport, listing tools
// and resources, and invoking tool calls. The SSE transport is not yet
// implemented and fails cleanly at construction time (see NewClient).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// maxMessageBytes bounds the size of a single JSON-RPC message read from the
// server. It guards against a misbehaving or malicious server streaming an
// unbounded line and exhausting client memory.
const maxMessageBytes = 16 << 20 // 16 MiB

// Transport defines how the client communicates with an MCP server.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
)

// deadlineReader is satisfied by readers whose underlying file descriptor
// supports read deadlines (e.g. *os.File pipes). It lets the client honor a
// per-call context by unblocking an in-flight read.
type deadlineReader interface {
	SetReadDeadline(t time.Time) error
}

// ServerConfig holds the connection configuration for an MCP server.
type ServerConfig struct {
	Name      string    `json:"name" yaml:"name"`
	Transport Transport `json:"transport" yaml:"transport"`
	Command   string    `json:"command,omitempty" yaml:"command,omitempty"`
	Args      []string  `json:"args,omitempty" yaml:"args,omitempty"`
	URL       string    `json:"url,omitempty" yaml:"url,omitempty"`
}

// ToolInfo describes a tool provided by an MCP server.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// ResourceInfo describes a resource provided by an MCP server.
type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// ResourceContent holds the content returned when reading a resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// jsonrpcRequest is a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse is a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Client communicates with an MCP server using JSON-RPC 2.0 over stdio.
//
// Concurrency model:
//   - mu serializes in-flight requests. It is held while writing a request and
//     reading its response. The read honors the per-call context via a read
//     deadline, so it never blocks indefinitely while holding mu.
//   - Close does NOT acquire mu. It force-kills the subprocess and clears any
//     read deadline, which unblocks any in-flight read so a hung server can be
//     torn down even while a call is stuck. The closed flag is atomic so it can
//     be observed without the lock.
type Client struct {
	config     ServerConfig
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     *bufio.Reader
	stdoutFile deadlineReader // underlying fd for read deadlines; nil if unsupported
	mu         sync.Mutex
	nextID     atomic.Int64
	closed     atomic.Bool
	info       ServerInfo
}

// ServerInfo holds the server's initialization response.
type ServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	ProtocolVer string `json:"protocolVersion"`
}

// NewClient creates a new MCP client for the given server configuration.
// For stdio transport, it launches the command as a subprocess on Connect.
//
// The SSE transport is not yet implemented; NewClient rejects it at
// construction time with a clear error rather than deferring the failure to
// call time.
func NewClient(cfg ServerConfig) (*Client, error) {
	if cfg.Transport == "" {
		cfg.Transport = TransportStdio
	}
	switch cfg.Transport {
	case TransportStdio:
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp: command is required for stdio transport")
		}
	case TransportSSE:
		return nil, fmt.Errorf("mcp: SSE transport is not implemented; use stdio transport (config %q)", cfg.Name)
	default:
		return nil, fmt.Errorf("mcp: unknown transport %q (supported: stdio)", cfg.Transport)
	}

	return &Client{config: cfg}, nil
}

// Connect starts the MCP server process and performs the initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// The subprocess lifecycle is managed explicitly through Close, so a
	// per-call ctx must not kill the shared process: use exec.Command (not
	// CommandContext) here.
	cmd := exec.Command(c.config.Command, c.config.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err = cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("mcp: start %q: %w", c.config.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)
	if dr, ok := stdout.(deadlineReader); ok {
		c.stdoutFile = dr
	}

	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "chronos",
			"version": "1.0.0",
		},
	}

	// callLocked is used here because we already hold c.mu.
	result, err := c.callLocked(ctx, "initialize", initParams)
	if err != nil {
		c.closeProcess()
		return fmt.Errorf("mcp: initialize: %w", err)
	}

	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(result, &initResult); err != nil {
		c.closeProcess()
		return fmt.Errorf("mcp: parse init result: %w", err)
	}

	c.info = ServerInfo{
		Name:        initResult.ServerInfo.Name,
		Version:     initResult.ServerInfo.Version,
		ProtocolVer: initResult.ProtocolVersion,
	}

	if err := c.notify("notifications/initialized", nil); err != nil {
		c.closeProcess()
		return fmt.Errorf("mcp: initialized notification: %w", err)
	}

	return nil
}

// ListTools fetches the available tools from the MCP server.
func (c *Client) ListTools(ctx context.Context) ([]ToolInfo, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/list: %w", err)
	}

	var resp struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp: parse tools: %w", err)
	}
	return resp.Tools, nil
}

// CallTool invokes a tool on the MCP server with the given arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return nil, fmt.Errorf("mcp: tools/call %q: %w", name, err)
	}

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		} `json:"content"`
		IsError bool `json:"isError,omitempty"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp: parse tool result: %w", err)
	}

	if resp.IsError {
		if len(resp.Content) > 0 {
			return nil, fmt.Errorf("mcp tool error: %s", resp.Content[0].Text)
		}
		return nil, fmt.Errorf("mcp tool error: unknown")
	}

	if len(resp.Content) == 1 {
		return resp.Content[0].Text, nil
	}

	texts := make([]string, len(resp.Content))
	for i := range resp.Content {
		texts[i] = resp.Content[i].Text
	}
	return texts, nil
}

// ListResources fetches the available resources from the MCP server.
func (c *Client) ListResources(ctx context.Context) ([]ResourceInfo, error) {
	result, err := c.call(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/list: %w", err)
	}

	var resp struct {
		Resources []ResourceInfo `json:"resources"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp: parse resources: %w", err)
	}
	return resp.Resources, nil
}

// ReadResource fetches the content of a resource by URI.
func (c *Client) ReadResource(ctx context.Context, uri string) ([]ResourceContent, error) {
	params := map[string]any{"uri": uri}

	result, err := c.call(ctx, "resources/read", params)
	if err != nil {
		return nil, fmt.Errorf("mcp: resources/read %q: %w", uri, err)
	}

	var resp struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("mcp: parse resource content: %w", err)
	}
	return resp.Contents, nil
}

// Info returns the server information from the initialize handshake.
func (c *Client) Info() ServerInfo {
	return c.info
}

// Close shuts down the MCP server connection. It force-kills the subprocess
// and clears any read deadline so that an in-flight call blocked on a hung
// server is unblocked promptly. Close does not acquire the request mutex, so
// it can tear down a client even while a call is stuck in a read. Close is
// idempotent and safe to call multiple times.
func (c *Client) Close() error {
	c.closeProcess()
	return nil
}

// closeProcess tears down the subprocess. It is idempotent: only the first
// caller performs teardown. It intentionally avoids acquiring c.mu so it can
// run concurrently with (and unblock) an in-flight callLocked read.
func (c *Client) closeProcess() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	// Unblock any in-flight read immediately; the read goroutine holds c.mu,
	// which we deliberately do not contend for.
	if c.stdoutFile != nil {
		_ = c.stdoutFile.SetReadDeadline(time.Now())
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// SIGKILL is uncatchable, so even a hung server is reaped and Wait
		// returns promptly once the OS tears the process down.
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
}

// call acquires c.mu and sends a JSON-RPC request, waiting for the matching response.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callLocked(ctx, method, params)
}

// callLocked sends a JSON-RPC request and waits for the response, honoring the
// per-call context. The caller must hold c.mu.
//
// The context is honored via a read deadline on the underlying stdout file
// descriptor (when supported): a context deadline is applied directly, and a
// watcher goroutine unblocks the read if the context is canceled without a
// deadline. This guarantees the read never blocks indefinitely while holding
// c.mu.
func (c *Client) callLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}

	id := c.nextID.Add(1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if c.closed.Load() {
		return nil, fmt.Errorf("client is closed")
	}

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	// Honor the per-call context through read deadlines when the underlying
	// reader supports them. Manually constructed clients (e.g. in tests) may
	// not set stdoutFile; in that case reads fall back to plain blocking.
	if c.stdoutFile != nil {
		if dl, ok := ctx.Deadline(); ok {
			_ = c.stdoutFile.SetReadDeadline(dl)
		}
		stop := make(chan struct{})
		watcherDone := make(chan struct{})
		go func() {
			defer close(watcherDone)
			select {
			case <-ctx.Done():
				// Interrupt the blocked read; ctx.Err() is checked below.
				_ = c.stdoutFile.SetReadDeadline(time.Now())
			case <-stop:
			}
		}()
		defer func() {
			close(stop)
			<-watcherDone
			// Clear the deadline so a subsequent call on this client is not
			// pre-expired.
			_ = c.stdoutFile.SetReadDeadline(time.Time{})
		}()
	}

	for {
		line, err := readMessage(c.stdout, maxMessageBytes)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return nil, fmt.Errorf("mcp: %s: %w", method, cerr)
			}
			// Close sets c.closed (a release) before tripping the read
			// deadline, so a closed client is observed here first — check it
			// before treating a timeout as ctx-driven, or we'd block on a
			// ctx that Close never cancels.
			if c.closed.Load() {
				return nil, fmt.Errorf("read: client is closed")
			}
			// A read-deadline timeout that is not from Close is only ever set
			// from the per-call ctx (its deadline, or the watcher on
			// cancellation). The fd deadline and the ctx timer are independent,
			// so the read can time out a hair before ctx.Err() is set; wait for
			// the definitive ctx error rather than surfacing the raw i/o
			// timeout.
			var timeoutErr interface{ Timeout() bool }
			if c.stdoutFile != nil && errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
				<-ctx.Done()
				return nil, fmt.Errorf("mcp: %s: %w", method, ctx.Err())
			}
			return nil, fmt.Errorf("read: %w", err)
		}

		var resp jsonrpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue
		}

		if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
			continue
		}

		if resp.ID != id {
			continue
		}

		if resp.Error != nil {
			return nil, fmt.Errorf("server error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		return resp.Result, nil
	}
}

// readMessage reads a single newline-delimited message from r, bounding the
// message size to limit bytes. The trailing newline is not included. It
// returns an error if the message exceeds the limit, preventing unbounded
// memory growth from a misbehaving server.
func readMessage(r *bufio.Reader, limit int) ([]byte, error) {
	buf := make([]byte, 0, 512)
	for {
		b, err := r.ReadByte()
		if err != nil {
			if len(buf) > 0 && err == io.EOF {
				// Return the partial line so a trailing message without a
				// newline is still parseable.
				return buf, nil
			}
			return nil, err
		}
		if b == '\n' {
			return buf, nil
		}
		buf = append(buf, b)
		if len(buf) > limit {
			return nil, fmt.Errorf("message exceeds %d byte limit", limit)
		}
	}
}

func (c *Client) notify(method string, params any) error {
	req := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	data = append(data, '\n')

	if _, err := c.stdin.Write(data); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	return nil
}
