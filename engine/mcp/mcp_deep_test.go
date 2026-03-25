package mcp

import (
	"bufio"
	"context"
	"os"
	"testing"

	"github.com/spawn08/chronos/engine/tool"
)

func TestNewClient_UnsupportedTransport_Deep(t *testing.T) {
	_, err := NewClient(ServerConfig{
		Name:      "x",
		Transport: TransportSSE,
		URL:       "http://localhost",
	})
	if err == nil {
		t.Fatal("expected unsupported transport error")
	}
}

func TestNewClient_MissingCommand_Deep(t *testing.T) {
	_, err := NewClient(ServerConfig{
		Name:      "x",
		Transport: TransportStdio,
		Command:   "",
	})
	if err == nil {
		t.Fatal("expected missing command error")
	}
}

func TestNewClient_DefaultTransport_Deep(t *testing.T) {
	_, err := NewClient(ServerConfig{
		Name:    "x",
		Command: "",
	})
	if err == nil {
		t.Fatal("expected error for empty command with default stdio")
	}
}

func TestToolInfoToDefinitions_Deep(t *testing.T) {
	c := &Client{config: ServerConfig{Name: "n"}}
	defs := ToolInfoToDefinitions(c, []ToolInfo{
		{Name: "t1", Description: "d", InputSchema: map[string]any{"type": "object"}},
	})
	if len(defs) != 1 || defs[0].Name != "t1" {
		t.Fatalf("defs=%v", defs)
	}
}

func TestRegisterTools_ListToolsError_Deep(t *testing.T) {
	// Pipes with stdin closed so tools/list write fails (no panic).
	_, clientW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	serverR, clientR, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = clientW.Close()

	c := &Client{
		config: ServerConfig{Name: "broken"},
		stdin:  clientW,
		stdout: bufio.NewReader(serverR),
	}
	reg := tool.NewRegistry()
	_, err = RegisterTools(context.Background(), c, reg)
	if err == nil {
		t.Fatal("expected register tools error")
	}
	_ = clientR.Close()
}
