package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spawn08/chronos/engine/mcp"
)

// TestLoadFileMCPServers verifies mcp_servers parse from YAML and that
// ${ENV_VAR} references in command/args/url are expanded.
func TestLoadFileMCPServers(t *testing.T) {
	t.Setenv("MCP_ROOT", "/srv/data")

	yaml := `
agents:
  - id: mcp-agent
    name: MCP Agent
    model:
      provider: ollama
      model: llama3.3
    mcp_servers:
      - name: filesystem
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "${MCP_ROOT}"]
      - name: remote
        transport: sse
        url: https://example.com/mcp
`
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write test config: %v", err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	servers := fc.Agents[0].MCPServers
	if len(servers) != 2 {
		t.Fatalf("expected 2 mcp servers, got %d", len(servers))
	}

	fs := servers[0]
	if fs.Name != "filesystem" {
		t.Errorf("expected name 'filesystem', got %q", fs.Name)
	}
	if fs.Transport != mcp.TransportStdio {
		t.Errorf("expected stdio transport, got %q", fs.Transport)
	}
	if fs.Command != "npx" {
		t.Errorf("expected command 'npx', got %q", fs.Command)
	}
	if got := fs.Args[len(fs.Args)-1]; got != "/srv/data" {
		t.Errorf("expected env-expanded arg '/srv/data', got %q", got)
	}
	if servers[1].URL != "https://example.com/mcp" {
		t.Errorf("unexpected url: %q", servers[1].URL)
	}
}

// TestBuildAgentRegistersMCPServers verifies BuildAgent records the configured
// MCP servers on the agent so ConnectMCP can later connect them.
func TestBuildAgentRegistersMCPServers(t *testing.T) {
	cfg := &AgentConfig{
		ID:   "mcp-build",
		Name: "MCP Build",
		Model: ModelConfig{
			Provider: "ollama",
			Model:    "llama3.3",
		},
		Storage: StorageConfig{Backend: "none"},
		MCPServers: []mcp.ServerConfig{
			{
				Name:    "filesystem",
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
			},
		},
	}

	a, err := BuildAgent(context.Background(), cfg)
	if err != nil {
		t.Fatalf("BuildAgent: %v", err)
	}
	if len(a.MCPClients) != 1 {
		t.Fatalf("expected 1 MCP client registered, got %d", len(a.MCPClients))
	}

	// CloseMCP must be safe even though the servers were never connected.
	a.CloseMCP()
}
