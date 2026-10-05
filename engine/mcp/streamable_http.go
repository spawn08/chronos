package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Streamable HTTP transport (MCP 2025-03-26): every JSON-RPC message is an
// HTTP POST to one endpoint. The server answers with either a single JSON
// body or a text/event-stream carrying the response (and possibly related
// notifications). The server may assign a session in the Mcp-Session-Id
// response header of the initialize call; the client echoes it on every
// later request and DELETEs it on Close.

const (
	streamableProtocolVersion = "2025-03-26"
	headerSessionID           = "Mcp-Session-Id"
	headerProtocolVersion     = "MCP-Protocol-Version"
	maxHTTPErrorBody          = 512
)

var envRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(:-([^}]*))?\}`)

// ExpandEnv expands ${VAR} and ${VAR:-default} from the environment. A
// variable that is unset (or empty with no default) is an error naming the
// variable, never its surrounding text, so secrets are not echoed.
func ExpandEnv(s string) (string, error) {
	var missing string
	out := envRefPattern.ReplaceAllStringFunc(s, func(ref string) string {
		m := envRefPattern.FindStringSubmatch(ref)
		if v, ok := os.LookupEnv(m[1]); ok && v != "" {
			return v
		}
		if m[2] != "" {
			return m[3]
		}
		if missing == "" {
			missing = m[1]
		}
		return ""
	})
	if missing != "" {
		return "", fmt.Errorf("environment variable %s is not set", missing)
	}
	return out, nil
}

// String renders the config for logs with header values and URL userinfo or
// query redacted. Use it (or %v / %s) instead of dumping the struct.
func (c ServerConfig) String() string {
	names := make([]string, 0, len(c.Headers))
	for name := range c.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Sprintf("mcp.ServerConfig{Name:%q Transport:%q Command:%q URL:%q Headers:%v}",
		c.Name, c.Transport, c.Command, RedactURL(c.URL), names)
}

// RedactURL removes userinfo and query values from a URL for display.
func RedactURL(raw string) string {
	if raw == "" {
		return raw
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i] + "?<redacted>"
	}
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if at := strings.Index(rest, "@"); at >= 0 && !strings.Contains(rest[:at], "/") {
			raw = raw[:i+3] + "<redacted>@" + rest[at+1:]
		}
	}
	return raw
}

func (c *Client) initStreamableHTTP() error {
	url, err := ExpandEnv(c.config.URL)
	if err != nil {
		return fmt.Errorf("mcp: url for %q: %w", c.config.Name, err)
	}
	c.config.URL = url
	if len(c.config.Headers) > 0 {
		headers := make(map[string]string, len(c.config.Headers))
		for name, value := range c.config.Headers {
			expanded, err := ExpandEnv(value)
			if err != nil {
				return fmt.Errorf("mcp: header %q for %q: %w", name, c.config.Name, err)
			}
			headers[name] = expanded
		}
		c.config.Headers = headers
	}
	c.httpClient = &http.Client{
		// Responses may stream; per-call contexts bound each request.
		Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			MaxIdleConns:        10,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
	return nil
}

func (c *Client) connectStreamableHTTP(ctx context.Context) error {
	initParams := map[string]any{
		"protocolVersion": streamableProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "chronos", "version": "1.0.0"},
	}
	result, err := c.callHTTP(ctx, "initialize", initParams)
	if err != nil {
		c.closeHTTP()
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
		c.closeHTTP()
		return fmt.Errorf("mcp: parse init result: %w", err)
	}
	c.info = ServerInfo{Name: initResult.ServerInfo.Name, Version: initResult.ServerInfo.Version, ProtocolVer: initResult.ProtocolVersion}
	c.pendingMu.Lock()
	c.protocolVersion = initResult.ProtocolVersion
	c.pendingMu.Unlock()
	if err := c.notifyHTTP("notifications/initialized", nil); err != nil {
		c.closeHTTP()
		return fmt.Errorf("mcp: initialized notification: %w", err)
	}
	return nil
}

// newHTTPRequest builds a POST with the standard and configured headers.
func (c *Client) newHTTPRequest(ctx context.Context, method string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.config.URL, reader)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	for name, value := range c.config.Headers {
		req.Header.Set(name, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	c.pendingMu.Lock()
	session, version := c.sessionID, c.protocolVersion
	c.pendingMu.Unlock()
	if session != "" {
		req.Header.Set(headerSessionID, session)
	}
	if version != "" {
		req.Header.Set(headerProtocolVersion, version)
	}
	return req, nil
}

// httpStatusError reports a non-success status without echoing request data.
func httpStatusError(resp *http.Response) error {
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPErrorBody))
	text := strings.TrimSpace(string(snippet))
	if text != "" {
		return fmt.Errorf("unexpected HTTP status %d: %s", resp.StatusCode, text)
	}
	return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
}

func (c *Client) callHTTP(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("client is closed")
	}
	id := c.nextID.Add(1)
	body, err := json.Marshal(jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := c.newHTTPRequest(ctx, http.MethodPost, body)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: post: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusNotFound && req.Header.Get(headerSessionID) != "" {
			return nil, fmt.Errorf("mcp: %s: session expired (HTTP 404)", method)
		}
		return nil, fmt.Errorf("mcp: %s: %w", method, httpStatusError(resp))
	}
	if session := resp.Header.Get(headerSessionID); session != "" && method == "initialize" {
		c.pendingMu.Lock()
		c.sessionID = session
		c.pendingMu.Unlock()
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	var message jsonrpcResponse
	switch {
	case strings.HasPrefix(contentType, "text/event-stream"):
		message, err = readStreamedResponse(resp.Body, id)
	default:
		var data []byte
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxMessageBytes+1))
		if err == nil && len(data) > maxMessageBytes {
			err = fmt.Errorf("message exceeds %d byte limit", maxMessageBytes)
		}
		if err == nil {
			err = json.Unmarshal(data, &message)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("mcp: %s: read response: %w", method, err)
	}
	if message.Error != nil {
		return nil, fmt.Errorf("mcp: %s: server error %d: %s", method, message.Error.Code, message.Error.Message)
	}
	return message.Result, nil
}

// readStreamedResponse reads an SSE body until the response with the wanted
// id arrives; notifications and other messages on the stream are skipped.
func readStreamedResponse(body io.Reader, id int64) (jsonrpcResponse, error) {
	reader := bufio.NewReaderSize(body, 64<<10)
	var data bytes.Buffer
	flush := func() (jsonrpcResponse, bool) {
		defer data.Reset()
		if data.Len() == 0 {
			return jsonrpcResponse{}, false
		}
		var message jsonrpcResponse
		if err := json.Unmarshal(data.Bytes(), &message); err != nil {
			return jsonrpcResponse{}, false
		}
		return message, message.ID == id && (message.Result != nil || message.Error != nil)
	}
	for {
		line, err := readMessage(reader, maxMessageBytes)
		line = bytes.TrimRight(line, "\r")
		switch {
		case len(line) == 0 && err == nil:
			if message, ok := flush(); ok {
				return message, nil
			}
		case len(line) > 0 && line[0] != ':':
			if field, value := splitSSEField(line); field == "data" {
				if data.Len() > 0 {
					data.WriteByte('\n')
				}
				data.Write(value)
			}
		}
		if err != nil {
			if message, ok := flush(); ok {
				return message, nil
			}
			if err == io.EOF {
				return jsonrpcResponse{}, fmt.Errorf("stream ended without a response")
			}
			return jsonrpcResponse{}, err
		}
	}
}

func (c *Client) notifyHTTP(method string, params any) error {
	if c.closed.Load() {
		return fmt.Errorf("client is closed")
	}
	body, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params,omitempty"`
	}{"2.0", method, params})
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := c.newHTTPRequest(ctx, http.MethodPost, body)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxMessageBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return nil
}

// closeHTTP ends the session (best effort DELETE) and releases connections.
// It is idempotent.
func (c *Client) closeHTTP() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.pendingMu.Lock()
	session := c.sessionID
	c.pendingMu.Unlock()
	if session != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if req, err := c.newHTTPRequest(ctx, http.MethodDelete, nil); err == nil {
			if resp, err := c.httpClient.Do(req); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				resp.Body.Close()
			}
		}
		cancel()
	}
	c.httpClient.CloseIdleConnections()
}
