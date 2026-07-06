// Example: mcp_agent demonstrates connecting an agent to a Model Context
// Protocol (MCP) server and importing its tools into the agent's registry.
//
// It launches the official filesystem MCP server over stdio, registers every
// tool the server exposes, and prints them. If an OpenAI API key is present,
// the agent then answers a prompt that can use those tools.
//
// Prerequisites (for a live run):
//
//	npm install -g @modelcontextprotocol/server-filesystem   # or use npx
//
// Run:
//
//	go run ./examples/mcp_agent/
//
// The example degrades gracefully: if the MCP server binary is not installed,
// it prints the connection error and exits 0 so it stays CI-safe.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spawn08/chronos/engine/mcp"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos MCP Agent Example                          ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── Build the agent with an MCP server ──
	//
	// The filesystem server is launched via npx over stdio. Its tools
	// (read_file, list_directory, ...) are scoped to the directory passed
	// as the final argument.
	builder := agent.New("mcp-demo", "MCP Demo Agent").
		WithSystemPrompt("You are a helpful assistant with filesystem tools.").
		AddMCPServer(mcp.ServerConfig{
			Name:      "filesystem",
			Transport: mcp.TransportStdio,
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
		})

	// Attach a model only if credentials are available.
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		builder = builder.WithModel(model.NewOpenAI(key))
	}

	a, err := builder.Build()
	if err != nil {
		log.Fatalf("build agent: %v", err)
	}

	// ── Connect MCP servers and import their tools ──
	//
	// ConnectMCP launches each server, performs the initialize handshake,
	// and registers every advertised tool in the agent's tool registry.
	if err := a.ConnectMCP(ctx); err != nil {
		fmt.Printf("\n⚠️  Could not connect MCP server: %v\n", err)
		fmt.Println("   Install it with: npm install -g @modelcontextprotocol/server-filesystem")
		return
	}
	defer a.CloseMCP()

	// ── Inspect the imported tools ──
	fmt.Println("\nTools imported from MCP servers:")
	for _, def := range a.Tools.List() {
		fmt.Printf("  • %-20s %s\n", def.Name, def.Description)
	}

	// ── Optional: let the model use them ──
	if a.Model == nil {
		fmt.Println("\n(set OPENAI_API_KEY to have the model call these tools)")
		return
	}

	resp, err := a.Chat(ctx, "List the files in the current directory.")
	if err != nil {
		log.Fatalf("chat: %v", err)
	}
	fmt.Printf("\nAgent: %s\n", resp.Content)
}
