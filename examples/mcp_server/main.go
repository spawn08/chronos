// Example: mcp_server exposes Chronos tools to any MCP host (Claude Desktop, an
// IDE, another agent framework) over stdio (WC-B-002). Point an MCP host at:
//
//	command: go   args: ["run", "./examples/mcp_server/"]
//
// The host will initialize, list the exposed tools, and call them. The "add"
// tool is auto-approved; "delete_all" is exposed with the default
// require-approval permission, so the host must approve it (this example wires an
// auto-approving approver for demonstration — a real deployment would prompt a
// human).
//
// To try it without a host, pipe JSON-RPC in:
//
//	printf '%s\n%s\n' \
//	  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
//	  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"add","arguments":{"a":2,"b":3}}}' \
//	  | go run ./examples/mcp_server/
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spawn08/chronos/engine/interop/mcpserver"
	"github.com/spawn08/chronos/engine/tool"
)

func main() {
	srv := mcpserver.New("chronos-tools", mcpserver.WithVersion("1.0.0"))

	// Auto-approved: a safe, read-only-style computation.
	srv.Expose(&tool.Definition{
		Name:        "add",
		Description: "Add two numbers.",
		Permission:  tool.PermAllow,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number"},
				"b": map[string]any{"type": "number"},
			},
			"required": []any{"a", "b"},
		},
		Handler: func(_ context.Context, args map[string]any) (any, error) {
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return map[string]any{"sum": a + b}, nil
		},
	})

	// Exposed with no explicit permission → defaults to require-approval, since it
	// is a destructive operation reachable by a remote host.
	srv.Expose(&tool.Definition{
		Name:        "delete_all",
		Description: "Delete everything (guarded by approval).",
		Parameters:  map[string]any{"type": "object"},
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return map[string]any{"deleted": true}, nil
		},
	})

	// A real server prompts a human here; this demo approves automatically so the
	// guarded tool is runnable end-to-end.
	srv.SetApprover(autoApprover{})

	if err := srv.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
		log.Fatalf("mcp server: %v", err)
	}
	fmt.Fprintln(os.Stderr, "mcp server: stdin closed, exiting")
}

// autoApprover approves every request; a production server would ask a human.
type autoApprover struct{}

func (autoApprover) RequestApproval(_ context.Context, _ string, _ map[string]any) (bool, error) {
	return true, nil
}
