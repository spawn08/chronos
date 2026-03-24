package mcp

import (
	"encoding/json"
	"testing"
)

func TestNewClient_StdioTransport(t *testing.T) {
	cfg := ServerConfig{
		Name:      "test-server",
		Transport: TransportStdio,
		Command:   "echo",
		Args:      []string{"hello"},
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.config.Name != "test-server" {
		t.Errorf("config.Name = %q", client.config.Name)
	}
}

func TestNewClient_DefaultTransport(t *testing.T) {
	cfg := ServerConfig{
		Name:    "default",
		Command: "echo",
	}

	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.config.Transport != TransportStdio {
		t.Errorf("transport = %q, want stdio", client.config.Transport)
	}
}

func TestNewClient_UnsupportedTransport(t *testing.T) {
	cfg := ServerConfig{
		Name:      "sse",
		Transport: TransportSSE,
		URL:       "http://localhost:8080",
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("expected error for SSE transport")
	}
}

func TestNewClient_NoCommand(t *testing.T) {
	cfg := ServerConfig{
		Name:      "no-cmd",
		Transport: TransportStdio,
	}

	_, err := NewClient(cfg)
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestServerInfo_Zero(t *testing.T) {
	cfg := ServerConfig{
		Name:    "test",
		Command: "echo",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	info := client.Info()
	if info.Name != "" {
		t.Errorf("expected empty name before connect, got %q", info.Name)
	}
}

func TestClient_CloseBeforeConnect(t *testing.T) {
	cfg := ServerConfig{
		Name:    "test",
		Command: "echo",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClient_DoubleClose(t *testing.T) {
	cfg := ServerConfig{
		Name:    "test",
		Command: "echo",
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_ = client.Close()
	if err := client.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestToolInfo_Fields(t *testing.T) {
	tool := ToolInfo{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
		},
	}

	if tool.Name != "read_file" {
		t.Errorf("Name = %q", tool.Name)
	}
	if tool.Description != "Read a file" {
		t.Errorf("Description = %q", tool.Description)
	}
	props, ok := tool.InputSchema["properties"]
	if !ok {
		t.Error("expected properties in InputSchema")
	}
	propsMap, ok := props.(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	if _, ok := propsMap["path"]; !ok {
		t.Error("expected path property")
	}
}

func TestResourceInfo_Fields(t *testing.T) {
	res := ResourceInfo{
		URI:         "file:///tmp/test.txt",
		Name:        "test.txt",
		Description: "Test file",
		MimeType:    "text/plain",
	}

	if res.URI != "file:///tmp/test.txt" {
		t.Errorf("URI = %q", res.URI)
	}
	if res.MimeType != "text/plain" {
		t.Errorf("MimeType = %q", res.MimeType)
	}
}

func TestToolInfoToJSON(t *testing.T) {
	tools := []ToolInfo{
		{
			Name:        "search",
			Description: "Search for items",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "create",
			Description: "Create an item",
		},
	}

	data, err := ToolInfoToJSON(tools)
	if err != nil {
		t.Fatalf("ToolInfoToJSON: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("expected non-empty JSON output")
	}

	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(parsed) != 2 {
		t.Errorf("expected 2 tool definitions, got %d", len(parsed))
	}
	if parsed[0]["type"] != "function" {
		t.Errorf("type = %q, want function", parsed[0]["type"])
	}
}
