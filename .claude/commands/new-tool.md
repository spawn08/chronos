Register a new tool in the Chronos tool registry.

The tool name/description is: $ARGUMENTS

## Instructions

1. Create a `tool.Definition` struct with:
   - `Name`: snake_case tool name
   - `Description`: what the tool does
   - `Parameters`: JSON Schema as `map[string]any` describing the expected arguments
   - `Permission`: one of `tool.PermAllow`, `tool.PermRequireApproval`, or `tool.PermDeny`
   - `Handler`: a `func(ctx context.Context, args map[string]any) (any, error)` that implements the tool

2. The tool definition pattern (from `engine/tool/registry.go`):
```go
&tool.Definition{
    Name:        "tool_name",
    Description: "What this tool does",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "param1": map[string]any{"type": "string", "description": "..."},
        },
        "required": []string{"param1"},
    },
    Permission: tool.PermAllow,
    Handler: func(ctx context.Context, args map[string]any) (any, error) {
        // implementation
        return result, nil
    },
}
```

3. Register it on an agent via the builder: `.AddTool(def)` or directly: `registry.Register(def)`
4. For high-risk tools, use `tool.PermRequireApproval` — requires an approval handler to be set on the registry
5. Run `go build ./...` to verify
