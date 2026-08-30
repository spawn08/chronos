Connect a Chronos agent to MCP (Model Context Protocol) tool servers.

The MCP integration is: $ARGUMENTS

## Instructions

1. Read the MCP client at `engine/mcp/client.go` to understand the transport options.

2. Chronos supports two MCP transports:

   | Transport | Use Case | Configuration |
   |-----------|----------|---------------|
   | `stdio` | Local tools (filesystem, git, DB) — spawns a subprocess | `command` + `args` |
   | `sse` | Remote tools (APIs, cloud services) — connects to HTTP endpoint | `url` |

3. Add MCP servers via YAML:

```yaml
agents:
  - id: "mcp-agent"
    name: "MCP-Powered Agent"
    description: "Agent with access to external tools via MCP"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      You have access to external tools via MCP servers.
      Use the available tools to accomplish the user's request.

    mcp_servers:
      # --- Filesystem access ---
      - name: "filesystem"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
        permission: "allow"

      # --- GitHub integration ---
      - name: "github"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-github"]
        permission: "require_approval"

      # --- Database access ---
      - name: "postgres"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-postgres", "${DATABASE_URL}"]
        permission: "require_approval"

      # --- Web search ---
      - name: "brave-search"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-brave-search"]
        permission: "allow"

      # --- Remote MCP server (SSE transport) ---
      - name: "custom-api"
        transport: "sse"
        url: "https://mcp.example.com/sse"
        permission: "allow"

      # --- Local Python MCP server ---
      - name: "data-analysis"
        transport: "stdio"
        command: "python"
        args: ["-m", "my_mcp_server"]
        permission: "allow"
```

4. For programmatic MCP setup in Go:

```go
package main

import (
    "context"
    "log"

    "github.com/spawn08/chronos/engine/mcp"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, err := sqlite.New("mcp-agent.db")
    if err != nil { log.Fatal(err) }
    defer store.Close()
    store.Migrate(ctx)

    // Define MCP server configs
    servers := []mcp.ServerConfig{
        {
            Name:       "filesystem",
            Transport:  "stdio",
            Command:    "npx",
            Args:       []string{"-y", "@modelcontextprotocol/server-filesystem", "/data"},
            Permission: "allow",
        },
        {
            Name:       "custom-api",
            Transport:  "sse",
            URL:        "https://mcp.example.com/sse",
            Permission: "allow",
        },
    }

    // Build agent with MCP servers
    a, err := agent.New("mcp-agent", "MCP Agent").
        WithModel(model.NewAnthropic("${ANTHROPIC_API_KEY}")).
        WithStorage(store).
        WithMCPServers(servers...).
        Build()
    if err != nil { log.Fatal(err) }

    // MCP tools are automatically discovered and registered
    result, err := a.Run(ctx, "List files in /data")
    if err != nil { log.Fatal(err) }
    fmt.Println(result)
}
```

5. Permission levels for MCP tools:

   | Permission | Behavior |
   |------------|----------|
   | `allow` | Tools execute without confirmation |
   | `require_approval` | Tools require human approval before execution |
   | `deny` | Tools from this server are blocked |

   Best practice: use `require_approval` for MCP servers that modify state (databases, file writes, API mutations).

6. Building your own MCP server for Chronos:

   Create a Python MCP server that Chronos can connect to:

```python
# my_mcp_server.py
from mcp.server import Server
from mcp.types import Tool, TextContent

app = Server("my-tools")

@app.tool()
async def analyze_data(query: str) -> list[TextContent]:
    """Analyze data based on a natural language query."""
    # Your analysis logic here
    result = f"Analysis result for: {query}"
    return [TextContent(type="text", text=result)]

if __name__ == "__main__":
    import asyncio
    asyncio.run(app.run_stdio())
```

   Then connect it:
```yaml
mcp_servers:
  - name: "data-analysis"
    transport: "stdio"
    command: "python"
    args: ["my_mcp_server.py"]
    permission: "allow"
```

7. Common MCP server packages (install with `npx -y`):

   | Package | Tools Provided |
   |---------|---------------|
   | `@modelcontextprotocol/server-filesystem` | read, write, list, search files |
   | `@modelcontextprotocol/server-github` | repos, issues, PRs, code search |
   | `@modelcontextprotocol/server-postgres` | SQL queries, schema inspection |
   | `@modelcontextprotocol/server-brave-search` | Web search |
   | `@modelcontextprotocol/server-memory` | Persistent key-value memory |
   | `@modelcontextprotocol/server-puppeteer` | Browser automation |
   | `@modelcontextprotocol/server-slack` | Slack messaging |

8. Run and verify:
```bash
# Verify MCP tools are discovered (agent id is positional, not -a; "info" doesn't exist, use "show")
go run ./cli/main.go agent show -c agents.yaml mcp-agent

# Run with MCP tools
go run ./cli/main.go agent chat -c agents.yaml mcp-agent
```

9. Troubleshooting:
   - If stdio transport fails, ensure the command is in PATH
   - If SSE transport fails, check the server URL is accessible
   - Enable `debug: true` on the agent to see MCP handshake logs
   - MCP servers must respond to the `initialize` handshake within the timeout
