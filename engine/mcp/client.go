// Package mcp implements the Model Context Protocol (MCP) client.
// It supports connecting to MCP servers via stdio or HTTP SSE transport,
// listing tools and resources, and invoking tool calls.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// Transport defines how the client communicates with an MCP server.
type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportSSE   Transport = "sse"
)

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
type Client struct {
	config    ServerConfig
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	mu        sync.Mutex
	nextID    atomic.Int64
	closed    bool
	info      ServerInfo
	tools     []ToolInfo
	resources []ResourceInfo
}

// ServerInfo holds the server's initialization response.
type ServerInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	ProtocolVer string `json:"protocolVersion"`
}

// NewClient creates a new MCP client for the given server configuration.
// For stdio transport, it launches the command as a subprocess.
func NewClient(cfg ServerConfig) (*Client, error) {
	if cfg.Transport == "" {
		cfg.Transport = TransportStdio
	}
	if cfg.Transport != TransportStdio {
		return nil, fmt.Errorf("mcp: transport %q not yet supported (only stdio)", cfg.Transport)
	}
	if cfg.Command == "" {
		return nil, fmt.Errorf("mcp: command is required for stdio transport")
	}

	return &Client{config: cfg}, nil
}

// Connect starts the MCP server process and performs the initialize handshake.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := exec.CommandContext(ctx, c.config.Command, c.config.Args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("mcp: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcp: start %q: %w", c.config.Command, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.stdout = bufio.NewReader(stdout)

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
	c.tools = resp.Tools
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

	var texts []string
	for _, c := range resp.Content {
		texts = append(texts, c.Text)
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
	c.resources = resp.Resources
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

// Close shuts down the MCP server connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeProcess()
}

func (c *Client) closeProcess() error {
	if c.closed {
		return nil
	}
	c.closed = true
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	return nil
}

func (c *Client) call(_ context.Context, method string, params any) (json.RawMessage, error) {
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

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, fmt.Errorf("client is closed")
	}

	if _, err := c.stdin.Write(data); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}

	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
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
