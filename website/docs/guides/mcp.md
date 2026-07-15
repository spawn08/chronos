---
title: "Model Context Protocol (MCP)"
---

The [Model Context Protocol](https://modelcontextprotocol.io) (MCP) is an open standard that lets applications expose **tools** and **resources** to LLM clients over a uniform JSON-RPC interface. Chronos ships an MCP **client** so an agent can connect to any MCP server, import its tools into the agent's tool registry, and let the model call them transparently — the same way it calls native Chronos tools.

This means you can plug in the growing ecosystem of MCP servers (filesystem, GitHub, Postgres, Slack, Puppeteer, and many more) without writing a single tool handler.

## How it works

```
┌────────────┐   tools/list, tools/call    ┌──────────────────┐
│  Chronos   │ ──────────────────────────▶ │   MCP Server     │
│  Agent     │   JSON-RPC 2.0 over stdio   │  (subprocess)    │
│            │ ◀────────────────────────── │  filesystem, ... │
└────────────┘        results               └──────────────────┘
```

1. You register one or more MCP servers on the agent.
2. `ConnectMCP` launches each server, performs the MCP `initialize` handshake, and calls `tools/list`.
3. Every advertised tool is wrapped as a `tool.Definition` and registered in the agent's registry. Its handler routes `tools/call` requests back through the MCP client.
4. From then on, the model sees MCP tools alongside native tools and can call them during `Chat`, `Run`, or graph execution.

## Transports

| Transport | Const | Status | Use case |
|-----------|-------|--------|----------|
| stdio | `mcp.TransportStdio` | ✅ Supported | Launch a local MCP server as a subprocess (default) |
| HTTP SSE | `mcp.TransportSSE` | 🚧 Planned | Connect to a remote MCP server over HTTP |

stdio is the default. If `Transport` is empty it defaults to stdio. Requesting SSE currently returns an error.

## Go builder API

Add an MCP server while building the agent, then call `ConnectMCP` once after `Build`:

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/mcp"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    a, err := agent.New("assistant", "Assistant").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant with filesystem access.").
        AddMCPServer(mcp.ServerConfig{
            Name:      "filesystem",
            Transport: mcp.TransportStdio,
            Command:   "npx",
            Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    // Launch servers and import their tools into the registry.
    if err := a.ConnectMCP(ctx); err != nil {
        log.Fatal(err)
    }
    defer a.CloseMCP() // shut down MCP subprocesses

    resp, err := a.Chat(ctx, "List the files in the current directory.")
    if err != nil {
        log.Fatal(err)
    }
    log.Println(resp.Content)
}
```

### Builder methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `AddMCPServer` | `AddMCPServer(cfg mcp.ServerConfig) *Builder` | Register an MCP server on the agent. Multiple servers can be added. |
| `ConnectMCP` | `(a *Agent) ConnectMCP(ctx context.Context) error` | Connect all registered servers and import their tools. Call **after** `Build`. |
| `CloseMCP` | `(a *Agent) CloseMCP()` | Disconnect all servers and terminate their subprocesses. |

:::note
`ConnectMCP` is a separate step (not part of `Build`) because it performs I/O — it launches processes and blocks on the initialize handshake. Always pair it with `defer a.CloseMCP()`.
:::

## YAML configuration

MCP servers are first-class in `.chronos/agents.yaml`. List them under `mcp_servers` on any agent. Command, args, and URL support `${ENV_VAR}` expansion.

```yaml
# .chronos/agents.yaml
agents:
  - id: assistant
    name: Assistant
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a helpful assistant.
    mcp_servers:
      - name: filesystem
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
      - name: github
        transport: stdio
        command: npx
        args: ["-y", "@modelcontextprotocol/server-github"]
```

### `mcp_servers` fields

| Field | YAML key | Description |
|-------|----------|-------------|
| Name | `name` | Logical name for the server (used in error messages). |
| Transport | `transport` | `stdio` (default) or `sse` (planned). |
| Command | `command` | Executable to launch for stdio transport (e.g. `npx`, `uvx`, a binary path). |
| Args | `args` | Arguments passed to the command. |
| URL | `url` | Endpoint for SSE transport (when supported). |

When you build agents with `agent.BuildAgent` / `agent.BuildAll`, the servers are registered automatically. You still call `ConnectMCP` before running:

```go
fc, _ := agent.LoadFile(".chronos/agents.yaml")
agents, _ := agent.BuildAll(ctx, fc)
a := agents["assistant"]
if err := a.ConnectMCP(ctx); err != nil {
    log.Fatal(err)
}
defer a.CloseMCP()
```

## Working with resources

Beyond tools, MCP servers can expose **resources** — readable content addressed by URI (files, database rows, API responses). The client exposes these directly:

```go
client, _ := mcp.NewClient(mcp.ServerConfig{
    Name:    "filesystem",
    Command: "npx",
    Args:    []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
})
if err := client.Connect(ctx); err != nil {
    log.Fatal(err)
}
defer client.Close()

// Discover resources
resources, _ := client.ListResources(ctx)
for _, r := range resources {
    fmt.Printf("%s (%s)\n", r.URI, r.MimeType)
}

// Read one
contents, _ := client.ReadResource(ctx, "file:///path/to/README.md")
for _, c := range contents {
    fmt.Println(c.Text)
}
```

### Client methods

| Method | Description |
|--------|-------------|
| `NewClient(cfg ServerConfig) (*Client, error)` | Create a client for a server config. |
| `Connect(ctx) error` | Launch the server and run the `initialize` handshake. |
| `ListTools(ctx) ([]ToolInfo, error)` | Fetch advertised tools (`tools/list`). |
| `CallTool(ctx, name, args) (any, error)` | Invoke a tool (`tools/call`). |
| `ListResources(ctx) ([]ResourceInfo, error)` | Fetch resources (`resources/list`). |
| `ReadResource(ctx, uri) ([]ResourceContent, error)` | Read a resource (`resources/read`). |
| `Info() ServerInfo` | Server name/version/protocol from the handshake. |
| `Close() error` | Terminate the server subprocess. |

## Registering tools manually

If you want more control (e.g. filter or rename tools before registering), use the adapter directly:

```go
import "github.com/spawn08/chronos/engine/mcp"

// Register every tool from a connected client into a registry:
n, err := mcp.RegisterTools(ctx, client, agent.Tools)

// Or convert to definitions without registering, for inspection:
tools, _ := client.ListTools(ctx)
defs := mcp.ToolInfoToDefinitions(client, tools)
```

## Permissions & approval

Imported MCP tools are ordinary `tool.Definition` entries, so they participate in the same [permission and approval](/guides/tools) flow as native tools.

MCP tools are registered with `tool.PermRequireApproval` **by default** — because they come from an external server, they route through the human-approval path unless you explicitly opt out. To auto-allow a specific MCP tool you trust, look it up after `ConnectMCP` and relax its policy:

```go
if def, ok := a.Tools.Get("list_files"); ok {
    def.Permission = tool.PermAllow // trust this read-only MCP tool
}
```

Conversely, the default already requires approval, so no action is needed to gate a destructive tool like `write_file`.

## Example

A complete runnable example lives in [`examples/mcp_agent`](https://github.com/spawn08/chronos/tree/main/examples/mcp_agent). It connects to the filesystem MCP server, prints the imported tools, and (with an API key set) lets the model use them:

```bash
go run ./examples/mcp_agent/
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `mcp: command is required for stdio transport` | No `command` set | Provide `command` (and `args`) in the server config. |
| `mcp connect "...": ... executable file not found` | Server binary not installed | Install the MCP server (e.g. `npm i -g @modelcontextprotocol/server-filesystem`) or ensure `npx`/`uvx` is on `PATH`. |
| `mcp: transport "sse" not yet supported` | Requested SSE | Use `stdio` for now; SSE is planned. |
| Tools don't appear | `ConnectMCP` not called | Call `a.ConnectMCP(ctx)` after `Build`. |

## See also

- [Tools & Function Calling](/guides/tools) — how tools are defined, permissioned, and executed
- [Agents](/guides/agents) — the agent builder and lifecycle
- [Configuration](/getting-started/configuration) — full `.chronos/agents.yaml` reference
