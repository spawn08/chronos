Set up observability (tracing, audit logs, monitoring) for Chronos agents in production.

The deployment context is: $ARGUMENTS

## Instructions

1. Read the tracing system at `os/trace/` and audit logging at `os/trace/audit.go`.

2. Chronos provides three observability pillars:

   | Pillar | Package | Purpose |
   |--------|---------|---------|
   | Tracing | `os/trace/` | Span collection for request lifecycle |
   | Audit logs | `storage.Storage` | Append-only event log for compliance |
   | Monitoring | `cli/cmd/monitor.go` | Live TUI dashboard for health/metrics/sessions |

3. Enable tracing in YAML config:

```yaml
agents:
  - id: "production-agent"
    name: "Production Agent"
    tracing: true              # enables span collection
    debug: false               # set true for verbose logging (dev only)
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    storage:
      backend: postgres        # production: use postgres for durable traces
      dsn: "${DATABASE_URL}"
```

4. Set up the ChronosOS server with observability endpoints:

```bash
# Start the control plane server
export CHRONOS_AUTH=jwt   # none (default) | jwt | apikey
export CHRONOS_SWAGGER=true
go run ./cli/main.go serve :8420
```

The server exposes:
- `GET /health` — health check (returns 200 OK)
- `GET /api/sessions` — list active sessions
- `GET /api/sessions/:id` — session details with trace data
- `GET /api/sessions/:id/events` — event stream for a session
- `GET /api/sessions/:id/traces` — trace spans for a session
- `GET /api/sessions/:id/audit` — audit log entries

5. For programmatic tracing in Go:

```go
package main

import (
    "context"
    "log"

    "github.com/spawn08/chronos/os/trace"
    "github.com/spawn08/chronos/storage/adapters/postgres"
)

func main() {
    ctx := context.Background()

    store, err := postgres.New("${DATABASE_URL}")
    if err != nil { log.Fatal(err) }
    defer store.Close()
    store.Migrate(ctx)

    // Create a trace collector
    collector := trace.NewCollector(store)

    // Start a span
    span := collector.StartSpan(ctx, "agent.run", map[string]any{
        "agent_id": "my-agent",
        "input":    "user query",
    })

    // ... agent execution ...

    // End span with result
    span.End(map[string]any{
        "output":    "agent response",
        "tokens_in": 150,
        "tokens_out": 320,
    })

    // Audit log (append-only, immutable)
    store.AppendAuditLog(ctx, storage.AuditEntry{
        SessionID: "session-123",
        Action:    "agent.tool_call",
        Details:   map[string]any{"tool": "search_web", "approved": true},
    })
}
```

6. Use the live monitor TUI:

```bash
# Real-time dashboard showing health, metrics, active sessions
go run ./cli/main.go monitor --addr http://localhost:8420 --interval 5s
```

The monitor displays:
- Server health status
- Active session count
- Recent events and traces
- Memory usage

7. For production deployments, wire these environment variables on the `serve` command:

```bash
# Server configuration
export CHRONOS_AUTH=jwt                    # none (default) | jwt | apikey
export CHRONOS_SWAGGER=true                # enable Swagger UI at /swagger/
export CHRONOS_CORS_ORIGINS="https://app.example.com"
export CHRONOS_SHARED_STATE=true           # enable shared state across agents

# Database (for durable traces and audit logs)
export DATABASE_URL="postgres://user:pass@host:5432/chronos?sslmode=require"
```

8. Audit log best practices:
   - Use `storage.AppendAuditLog` for every tool call, approval decision, and error
   - Audit entries are immutable — they can be queried but never modified
   - Store sensitive decisions (permission escalations, data access) with full context
   - Use `storage.ListAuditLogs` with session filters for compliance reporting

9. Verify the setup:
```bash
# Start server with tracing
go run ./cli/main.go serve :8420

# In another terminal, run an agent
go run ./cli/main.go run -c agents.yaml -a my-agent "test query"

# Check traces
curl http://localhost:8420/api/sessions | jq .

# Start monitor
go run ./cli/main.go monitor --addr http://localhost:8420
```
