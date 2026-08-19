package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/tool"
)

// RegisterTools fetches tools from the MCP server and registers them
// in the given tool registry. Each MCP tool becomes a tool.Definition
// whose handler routes calls through the MCP client.
//
// The per-tool permission comes from the server's ServerConfig.Permission
// when set (e.g. "allow" for a trusted read-only server), otherwise it
// falls back to PermRequireApproval since MCP tools are externally provided.
func RegisterTools(ctx context.Context, client *Client, registry *tool.Registry) (int, error) {
	tools, err := client.ListTools(ctx)
	if err != nil {
		return 0, fmt.Errorf("mcp adapter: list tools: %w", err)
	}

	perm := tool.PermRequireApproval
	if raw := client.Config().Permission; raw != "" {
		p := tool.Permission(raw)
		switch p {
		case tool.PermAllow, tool.PermRequireApproval:
			perm = p
		default:
			return 0, fmt.Errorf("mcp adapter: server %q: invalid permission %q (want %q or %q)",
				client.Config().Name, raw, tool.PermAllow, tool.PermRequireApproval)
		}
	}

	for _, t := range tools {
		mcpTool := t
		mcpClient := client

		def := &tool.Definition{
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			Parameters:  mcpTool.InputSchema,
			Permission:  perm,
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return mcpClient.CallTool(ctx, mcpTool.Name, args)
			},
		}
		registry.Register(def)
	}

	return len(tools), nil
}

// ToolInfoToDefinitions converts MCP ToolInfo items into tool.Definition
// items without registering them. Useful for inspection or custom registration.
func ToolInfoToDefinitions(client *Client, tools []ToolInfo) []*tool.Definition {
	defs := make([]*tool.Definition, len(tools))
	for i, t := range tools {
		mcpTool := t
		mcpClient := client
		defs[i] = &tool.Definition{
			Name:        mcpTool.Name,
			Description: mcpTool.Description,
			Parameters:  mcpTool.InputSchema,
			// MCP tools are externally provided; default to requiring approval.
			Permission: tool.PermRequireApproval,
			Handler: func(ctx context.Context, args map[string]any) (any, error) {
				return mcpClient.CallTool(ctx, mcpTool.Name, args)
			},
		}
	}
	return defs
}

// ToolInfoToJSON converts tool info into a JSON representation suitable
// for model tool definitions.
func ToolInfoToJSON(tools []ToolInfo) ([]byte, error) {
	type functionDef struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Parameters  map[string]any `json:"parameters"`
	}
	type toolDef struct {
		Type     string      `json:"type"`
		Function functionDef `json:"function"`
	}

	defs := make([]toolDef, len(tools))
	for i, t := range tools {
		defs[i] = toolDef{
			Type: "function",
			Function: functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}

	return json.Marshal(defs)
}
