# MCP Client Transports

Chronos speaks the [Model Context Protocol](https://modelcontextprotocol.io) (MCP) as a client, so an agent can import tools and resources from any MCP server. Two transports are supported:

| Transport | Constant | How it connects |
|-----------|----------|-----------------|
| stdio | `TransportStdio` | Launches the server as a subprocess and exchanges JSON-RPC over its stdin/stdout |
| HTTP + SSE | `TransportSSE` | Opens a Server-Sent Events stream to a remote server and POSTs JSON-RPC requests to the endpoint it advertises (MCP 2024-11-05) |

Both transports expose the same client API: `Connect`, `ListTools`, `CallTool`, `ListResources`, `ReadResource`, `Info`, `Close`.

---

## stdio transport

Best for local servers distributed as executables (for example the official `@modelcontextprotocol/server-*` packages).

```go
client, err := mcp.NewClient(mcp.ServerConfig{
    Name:      "filesystem",
    Transport: mcp.TransportStdio,
    Command:   "npx",
    Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
})
if err != nil { /* ... */ }
defer client.Close()

if err := client.Connect(ctx); err != nil { /* ... */ }
tools, _ := client.ListTools(ctx)
```

Wire it into an agent so the server's tools are registered automatically:

```go
a, _ := agent.New("demo", "Demo").
    AddMCPServer(mcp.ServerConfig{
        Name: "filesystem", Transport: mcp.TransportStdio,
        Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "."},
    }).
    Build()
_ = a.ConnectMCP(ctx) // launches servers, handshakes, imports tools
```

Run the example (needs an LLM key to answer prompts; the connect/list step works without one):

```bash
go run ./examples/mcp_agent/
```

---

## HTTP + SSE transport

Best for **remote** servers reached over HTTP. The client opens a long-lived SSE stream (`GET` with `Accept: text/event-stream`); the server's first event names the URL to `POST` JSON-RPC requests to, and responses stream back as `message` events, correlated to requests by id.

```go
client, err := mcp.NewClient(mcp.ServerConfig{
    Name:      "remote-tools",
    Transport: mcp.TransportSSE,
    URL:       "https://mcp.example.com/sse", // required for SSE
})
if err != nil { /* SSE requires a URL */ }
defer client.Close()

if err := client.Connect(ctx); err != nil { /* ... */ }
tools, _ := client.ListTools(ctx)
out, _ := client.CallTool(ctx, "echo", map[string]any{"text": "hi"})
```

It also plugs into the agent builder — just set `Transport: mcp.TransportSSE` and a `URL` instead of a command.

Run the self-contained example (starts a demo SSE server in-process, no keys, no external service):

```bash
go run ./examples/mcp_sse/
```

### Behavior notes

- **Per-call timeouts** are honored: a `context` deadline or cancellation unblocks a pending call and cleans up its correlation entry.
- **`Close`** cancels the background stream reader, closes idle connections, and unblocks any in-flight call. It is idempotent.
- Both transports bound a single JSON-RPC message to 16 MiB to guard against a misbehaving server.
