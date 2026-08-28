---
title: "Tools & Function Calling"
---


Tools let agents call external functions during conversation. Chronos integrates with OpenAI-style function calling: the model requests tool invocations, the agent executes them, and results are sent back for the model to incorporate into its response.

## tool.Definition

Each tool is defined by a `tool.Definition`:

| Field | Type | Description |
|-------|------|--------------|
| `Name` | string | Unique tool identifier (used by the model) |
| `Description` | string | Description for the model to decide when to call |
| `Parameters` | map[string]any | JSON Schema for arguments |
| `Permission` | Permission | Execution policy: allow, require_approval, or deny |
| `Handler` | Handler | `func(ctx context.Context, args map[string]any) (any, error)` |

Example:

```go
import (
    "context"

    "github.com/spawn08/chronos/engine/tool"
)

var weatherTool = &tool.Definition{
    Name:        "get_weather",
    Description: "Get the current weather for a location",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "location": map[string]any{
                "type":        "string",
                "description": "City name, e.g. San Francisco",
            },
            "unit": map[string]any{
                "type":        "string",
                "enum":        []string{"celsius", "fahrenheit"},
                "description": "Temperature unit",
            },
        },
        "required": []string{"location"},
    },
    Permission: tool.PermAllow,
    Handler: func(ctx context.Context, args map[string]any) (any, error) {
        location, _ := args["location"].(string)
        unit, _ := args["unit"].(string)
        if unit == "" {
            unit = "celsius"
        }
        // Call weather API, return result
        return map[string]any{
            "location": location,
            "temp":     22,
            "unit":     unit,
        }, nil
    },
}
```

## Permissions

| Permission | Value | Behavior |
|------------|-------|----------|
| `PermAllow` | `"allow"` | Auto-approved; executes immediately |
| `PermRequireApproval` | `"require_approval"` | Blocks until `ApprovalFunc` returns true |
| `PermDeny` | `"deny"` | Always blocked |

### YAML permissions

YAML-defined tools can override a built-in's default permission:

```yaml
agents:
  - id: developer
    name: Developer
    permission_mode: prompt
    tools:
      - name: file_read
        permission: allow
      - name: file_write
        permission: require_approval
      - name: shell
        permission: deny
```

`permission_mode` controls approval-gated tools for that agent:

| Mode | Behavior |
|------|----------|
| `prompt` | Invoke the approval handler (default) |
| `auto_approve` | Skip approval/confirmation prompts, while still respecting explicit `deny` |
| `deny` | Reject approval-gated tools without prompting |

The optional `requires_confirmation` and `requires_user_input` fields override those flags on a tool definition. If a tool both requires approval and confirmation, one successful approval satisfies the confirmation for that call—Chronos does not prompt twice.

## tool.Registry

The registry manages tool definitions, permissions, and execution:

| Method | Description |
|--------|--------------|
| `Register(def *Definition)` | Add a tool |
| `List()` | Return all registered tools |
| `Execute(ctx, name, args)` | Run a tool by name (enforces permissions) |
| `SetApprovalHandler(fn ApprovalFunc)` | Set handler for `PermRequireApproval` tools |
| `SetPermissionMode(mode)` | Set `prompt`, `auto_approve`, or `deny` handling for approval-gated tools |
| `PermissionMode()` | Return the current registry permission mode |

## Adding Tools via Builder

Register tools when building an agent:

```go
import (
    "context"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/engine/tool"
    "github.com/spawn08/chronos/sdk/agent"
)

func buildToolAgent(provider model.Provider) (*agent.Agent, error) {
    return agent.New("tool-agent", "Tool Agent").
        WithModel(provider).
        AddTool(&tool.Definition{
            Name:        "get_weather",
            Description: "Get weather for a location",
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "location": map[string]any{
                        "type":        "string",
                        "description": "City name",
                    },
                },
                "required": []string{"location"},
            },
            Permission: tool.PermAllow,
            Handler: func(ctx context.Context, args map[string]any) (any, error) {
                loc, _ := args["location"].(string)
                return map[string]any{"location": loc, "temp": 20}, nil
            },
        }).
        Build()
}
```

## Automatic Tool-Call Loop

In `Chat` and `ChatWithSession`, when the model returns `StopReason == StopReasonToolCall` and `ToolCalls` is non-empty, the agent:

1. Appends the assistant message (with tool calls) to the conversation
2. For each tool call: fires `tool_call.before` hook, executes the tool, fires `tool_call.after` hook
3. Appends each tool result as a `tool`-role message
4. Sends the updated conversation back to the model
5. Repeats until the model returns a final text response

No extra code is required; the loop runs automatically.

## Hook Events

Tool execution fires these hook events:

| Event | When |
|-------|------|
| `tool_call.before` | Before each tool execution |
| `tool_call.after` | After each tool execution |

The event `Name` is the tool name; `Input` holds the arguments; `Output` holds the result; `Error` holds any execution error.

```go
type Event struct {
    Type     EventType
    Name     string  // tool name
    Input    any     // args
    Output   any     // result
    Error    error
    Metadata map[string]any
}
```

## Approval Flow

For tools with `PermRequireApproval`, set an approval handler on the registry. The handler blocks until the user approves or denies.

```go
import (
    "context"
    "log"

    "github.com/spawn08/chronos/sdk/agent"
)

func setApprovalHandler(a *agent.Agent) {
    a.Tools.SetApprovalHandler(func(ctx context.Context, toolName string, args map[string]any) (bool, error) {
        // In production: show UI, wait for user decision
        log.Printf("Approval requested for %s with args %v", toolName, args)
        return true, nil // true = approved
    })
}
```

If no handler is set and a tool requires approval, execution returns an error.

In the Chronos CLI, enter `a` at a prompt to approve the current call and auto-approve later approval-gated tools for the rest of the session:

```text
Approve? [y/N/a=all for session]: a
```

You can also choose a process-wide mode:

```bash
chronos --permission-mode auto_approve repl
chronos --dangerously-skip-permissions run "perform the trusted task"
chronos --permission-mode deny pipe
```

The dangerous shortcut never overrides a tool declared with `permission: deny`. See [CLI Runtime Controls](/guides/cli-runtime-controls) for precedence and non-interactive behavior.

## Complete Example: Weather Tool

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/engine/tool"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    weatherTool := &tool.Definition{
        Name:        "get_weather",
        Description: "Get the current weather for a city. Use this when the user asks about weather.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "location": map[string]any{
                    "type":        "string",
                    "description": "City name, e.g. San Francisco, London",
                },
                "unit": map[string]any{
                    "type":        "string",
                    "enum":        []string{"celsius", "fahrenheit"},
                    "description": "Temperature unit (default: celsius)",
                },
            },
            "required": []string{"location"},
        },
        Permission: tool.PermAllow,
        Handler: func(ctx context.Context, args map[string]any) (any, error) {
            location, _ := args["location"].(string)
            unit, _ := args["unit"].(string)
            if unit == "" {
                unit = "celsius"
            }
            // Simulated response
            return map[string]any{
                "location":    location,
                "temperature": 22,
                "unit":        unit,
                "condition":   "Partly cloudy",
            }, nil
        },
    }

    a, err := agent.New("weather-agent", "Weather Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You help users with weather information. Use the get_weather tool when asked about weather.").
        AddTool(weatherTool).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What's the weather like in Tokyo?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp.Content)
}
```

## JSON Schema for Parameters

Parameters must follow JSON Schema. The model uses this to generate valid arguments. Minimal schema:

```go
Parameters: map[string]any{
    "type": "object",
    "properties": map[string]any{
        "param_name": map[string]any{
            "type":        "string",
            "description": "What this parameter does",
        },
    },
    "required": []string{"param_name"},
}
```

Supported types: `string`, `number`, `integer`, `boolean`, `array`, `object`. Use `enum` for fixed choices.
