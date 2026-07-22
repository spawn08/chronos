// Package mcp implements the Model Context Protocol (MCP) client.
// It supports connecting to MCP servers over two transports, listing tools and
// resources, and invoking tool calls:
//
//   - stdio: JSON-RPC 2.0 framed as newline-delimited messages over a
//     subprocess's stdin/stdout. Requests are serialized and each call reads its
//     own response, honoring the per-call context via read deadlines.
//   - sse (HTTP+SSE, MCP 2024-11-05): the client opens a long-lived
//     text/event-stream GET to the server URL. The server's first event
//     (event: endpoint) names the URL to POST JSON-RPC requests to; subsequent
//     (event: message) events carry JSON-RPC responses and notifications. A
//     background reader correlates responses to waiting callers by JSON-RPC id.
//
// The high-level methods (ListTools, CallTool, ListResources, ReadResource)
// work identically over both transports.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
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
//
// The SSE transport adds an HTTP client, the POST endpoint advertised by the
// server, a background-reader cancel function, and a correlation map keyed by
// JSON-RPC id that routes streamed responses back to waiting callers. These
// fields are nil/zero for stdio clients.
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

	// SSE transport state (nil/zero for stdio clients).
	httpClient    *http.Client
	sseCtx        context.Context
	sseCancel     context.CancelFunc
	endpointReady chan struct{}
	// pendingMu guards endpointURL and pending.
	pendingMu   sync.Mutex
	endpointURL string
	pending     map[int64]chan jsonrpcResponse
}

// ServerInfo holds the server's initialization response.
type ServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	ProtocolVer string `json:"protocolVersion"`
}

// NewClient creates a new MCP client for the given server configuration.
// For stdio transport, it launches the command as a subprocess on Connect.
// For SSE transport, it opens the HTTP event stream on Connect; cfg.URL is
// required and validated here rather than deferring the failure to call time.
func NewClient(cfg ServerConfig) (*Client, error) {
	if cfg.Transport == "" {
		cfg.Transport = TransportStdio
	}
	c := &Client{config: cfg}
	switch cfg.Transport {
	case TransportStdio:
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp: command is required for stdio transport")
		}
	case TransportSSE:
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp: url is required for sse transport (config %q)", cfg.Name)
		}
		c.httpClient = &http.Client{
			// No client-level timeout: the SSE GET is a long-lived stream.
			// Per-call POSTs and the endpoint handshake are bounded by their
			// own context instead.
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		}
	default:
		return nil, fmt.Errorf("mcp: unknown transport %q (supported: stdio, sse)", cfg.Transport)
	}

	return c, nil
}

// Connect establishes the transport and performs the initialize handshake.
// For stdio it starts the subprocess; for SSE it opens the event stream and
// waits for the endpoint event before handshaking.
func (c *Client) Connect(ctx context.Context) error {
	if c.config.Transport == TransportSSE {
		return c.connectSSE(ctx)
	}

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
	if c.config.Transport == TransportSSE {
		c.closeSSE()
		return nil
	}
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

// call sends a JSON-RPC request and waits for the matching response, dispatching
// on the configured transport. For stdio it serializes on c.mu and reads the
// response inline; for SSE it POSTs the request and awaits the correlated
// response delivered over the event stream.
func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.config.Transport == TransportSSE {
		return c.callSSE(ctx, method, params)
	}
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
	if c.config.Transport == TransportSSE {
		return c.notifySSE(method, params)
	}
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

// connectSSE opens the long-lived SSE stream, waits for the endpoint event
// (bounded by ctx), then performs the JSON-RPC initialize handshake and sends
// notifications/initialized over the same POST endpoint.
func (c *Client) connectSSE(ctx context.Context) error {
	// The stream lifecycle is owned by Close, not the per-call ctx, so the GET
	// uses a background context that Close cancels via c.sseCancel.
	sseCtx, cancel := context.WithCancel(context.Background())
	c.sseCtx = sseCtx
	c.sseCancel = cancel
	c.pending = make(map[int64]chan jsonrpcResponse)
	c.endpointReady = make(chan struct{})

	req, err := http.NewRequestWithContext(sseCtx, http.MethodGet, c.config.URL, http.NoBody)
	if err != nil {
		cancel()
		return fmt.Errorf("mcp: sse request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("mcp: sse connect: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return fmt.Errorf("mcp: sse connect: unexpected status %d", resp.StatusCode)
	}

	go c.readSSE(resp)

	// Wait for the server to advertise the POST endpoint before handshaking.
	select {
	case <-c.endpointReady:
	case <-ctx.Done():
		c.closeSSE()
		return fmt.Errorf("mcp: sse endpoint: %w", ctx.Err())
	case <-sseCtx.Done():
		// The reader exited (EOF/error) before advertising an endpoint.
		return fmt.Errorf("mcp: sse stream closed before endpoint event")
	}

	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "chronos",
			"version": "1.0.0",
		},
	}

	result, err := c.call(ctx, "initialize", initParams)
	if err != nil {
		c.closeSSE()
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
		c.closeSSE()
		return fmt.Errorf("mcp: parse init result: %w", err)
	}

	c.info = ServerInfo{
		Name:        initResult.ServerInfo.Name,
		Version:     initResult.ServerInfo.Version,
		ProtocolVer: initResult.ProtocolVersion,
	}

	if err := c.notify("notifications/initialized", nil); err != nil {
		c.closeSSE()
		return fmt.Errorf("mcp: initialized notification: %w", err)
	}

	return nil
}

// readSSE consumes the event stream, resolving the endpoint event and routing
// message events to waiting callers. It runs until the stream ends or Close
// cancels sseCtx, then tears down: closing the body and canceling sseCtx so a
// blocked connectSSE handshake is released.
func (c *Client) readSSE(resp *http.Response) {
	defer func() {
		_ = resp.Body.Close()
		// Ensure connectSSE's endpoint wait and Close observe the exit.
		c.sseCancel()
	}()

	r := bufio.NewReader(resp.Body)
	var eventType string
	var data []byte
	endpointSet := false

	for {
		line, err := readMessage(r, maxMessageBytes)
		if err != nil {
			return
		}
		// SSE frames may be CRLF-terminated; readMessage strips only the LF.
		line = bytes.TrimSuffix(line, []byte("\r"))

		// A blank line terminates the current event; dispatch it.
		if len(line) == 0 {
			if len(data) > 0 || eventType != "" {
				switch eventType {
				case "endpoint":
					if !endpointSet {
						endpointSet = true
						c.setEndpoint(string(data))
					}
				default: // "message" or an unnamed event
					c.dispatchMessage(data)
				}
			}
			eventType = ""
			data = data[:0]
			continue
		}

		// Comment line (starts with ':') per the SSE spec.
		if line[0] == ':' {
			continue
		}

		field, value := splitSSEField(line)
		switch field {
		case "event":
			eventType = string(value)
		case "data":
			if len(data) > 0 {
				data = append(data, '\n')
			}
			data = append(data, value...)
			// Bound the buffered event to guard against unbounded memory use.
			if len(data) > maxMessageBytes {
				return
			}
		}
	}
}

// splitSSEField splits an SSE line into its field name and value. Per the SSE
// spec, a single leading space after the colon is stripped from the value.
func splitSSEField(line []byte) (field string, value []byte) {
	i := bytes.IndexByte(line, ':')
	if i < 0 {
		return string(line), nil
	}
	field = string(line[:i])
	value = line[i+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return field, value
}

// setEndpoint resolves the advertised POST endpoint against cfg.URL and signals
// connectSSE that the handshake may proceed.
func (c *Client) setEndpoint(raw string) {
	resolved := strings.TrimSpace(raw)
	if base, err := url.Parse(c.config.URL); err == nil {
		if ref, rerr := url.Parse(resolved); rerr == nil {
			resolved = base.ResolveReference(ref).String()
		}
	}

	c.pendingMu.Lock()
	c.endpointURL = resolved
	c.pendingMu.Unlock()

	close(c.endpointReady)
}

// dispatchMessage parses a JSON-RPC message from an SSE data payload and routes
// responses to the waiting caller. Notifications (no id) are ignored.
func (c *Client) dispatchMessage(data []byte) {
	var resp jsonrpcResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.ID == 0 && resp.Result == nil && resp.Error == nil {
		return
	}

	c.pendingMu.Lock()
	ch, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.pendingMu.Unlock()

	if ok {
		// ch is buffered (cap 1) and each id is served once, so this never
		// blocks and never sends on a closed channel (Close deletes entries
		// under pendingMu before closing them).
		ch <- resp
	}
}

// callSSE POSTs a JSON-RPC request to the server endpoint and waits for the
// correlated response delivered over the event stream, honoring the per-call
// context (cancellation/deadline unblocks the wait and cleans up the map entry).
func (c *Client) callSSE(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}
	if c.closed.Load() {
		return nil, fmt.Errorf("mcp: %s: client is closed", method)
	}

	id := c.nextID.Add(1)
	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	ch := make(chan jsonrpcResponse, 1)
	c.pendingMu.Lock()
	endpoint := c.endpointURL
	c.pending[id] = ch
	c.pendingMu.Unlock()

	cleanup := func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}

	if err := c.postSSE(ctx, endpoint, body); err != nil {
		cleanup()
		return nil, fmt.Errorf("mcp: %s: %w", method, err)
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("mcp: %s: client is closed", method)
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("server error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		cleanup()
		return nil, fmt.Errorf("mcp: %s: %w", method, ctx.Err())
	}
}

// notifySSE POSTs a JSON-RPC notification to the server endpoint without waiting
// for a response.
func (c *Client) notifySSE(method string, params any) error {
	if c.closed.Load() {
		return fmt.Errorf("mcp: client is closed")
	}
	req := struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	c.pendingMu.Lock()
	endpoint := c.endpointURL
	c.pendingMu.Unlock()

	ctx := c.sseCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.postSSE(ctx, endpoint, body); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}
	return nil
}

// postSSE sends a JSON-RPC body to the server's POST endpoint. The response
// body is drained and discarded: the actual JSON-RPC reply arrives over the
// event stream, not this POST (per the MCP 2024-11-05 HTTP+SSE transport).
func (c *Client) postSSE(ctx context.Context, endpoint string, body []byte) error {
	if endpoint == "" {
		return fmt.Errorf("no endpoint advertised by server")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("post: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// closeSSE tears down the SSE transport. It cancels the background reader,
// closes idle HTTP connections, and unblocks any in-flight waiters with an
// error. It is idempotent: only the first caller performs teardown.
func (c *Client) closeSSE() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	if c.sseCancel != nil {
		c.sseCancel()
	}
	if c.httpClient != nil {
		c.httpClient.CloseIdleConnections()
	}
	// Unblock any in-flight waiters. Closing a pending channel signals the
	// waiter to return a "closed" error; deleting the entry under the lock
	// prevents a concurrent dispatch from sending on a closed channel.
	c.pendingMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pendingMu.Unlock()
}
