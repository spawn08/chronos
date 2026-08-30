---
name: chronos-production
description: Production-harden Chronos agents — authentication (JWT/API-key/RBAC), guardrails, hooks, evaluation suites, and observability. Each concern has ready-to-copy files in examples/.
---

# Chronos Production Hardening

## Activation
Use this skill when:
- Securing Chronos agents for production (auth, permissions)
- Adding input/output validation (guardrails)
- Setting up middleware (hooks)
- Writing agent evaluation/test suites
- Configuring tracing, audit logs, or monitoring

## Concern Selection

| Concern | Directory | What it covers |
|---------|-----------|----------------|
| **Auth** | `examples/auth/` | JWT, API keys, RBAC, CORS, permission modes |
| **Guardrails** | `examples/guardrails/` | Input/output validation, blocklist, length limits |
| **Hooks** | `examples/hooks/` | Before/after middleware for logging, metrics, audit |
| **Evals** | `examples/evals/` | Agent quality test suites |
| **Observability** | `examples/observability/` | Tracing, audit logs, monitoring dashboard |

---

## Concern: Auth

**Files:** `examples/auth/env.example`

### Server Configuration
```bash
CHRONOS_AUTH=jwt                               # none (default) | jwt | apikey
CHRONOS_JWT_SECRET="random-32-char-secret"     # HS256 signing key (jwt mode)
CHRONOS_JWT_ISSUER="chronos"                   # optional, enforced if set
CHRONOS_JWT_AUDIENCE="chronos-api"             # optional, enforced if set
CHRONOS_JWT_JWKS_URL="https://issuer/.well-known/jwks.json"  # RS256/OIDC, jwt mode
CHRONOS_API_KEYS="key-1-xxx:admin:tenant-a,key-2-yyy"        # "key:role:tenant" (apikey mode)
CHRONOS_RBAC=true                              # enforce role checks (needs auth enabled)
CHRONOS_CORS_ORIGINS="https://app.example.com"
CHRONOS_SWAGGER=false                          # disable in production
```

There is no `CHRONOS_JWT_EXPIRY` — a JWT's expiry is baked into the token itself (`exp` claim) at mint time, not configured server-side.

### Minting a credential
`chronos auth token` mints one matching whatever `CHRONOS_AUTH` mode is active, so you never hand-craft a key or sign a JWT yourself:
```bash
CHRONOS_AUTH=apikey chronos auth token --role admin --tenant tenant-a
# prints a ready-to-use CHRONOS_API_KEYS entry + curl example

CHRONOS_AUTH=jwt CHRONOS_JWT_SECRET=random-32-char-secret chronos auth token --role admin --ttl 24h
# prints a signed HS256 JWT + curl example
```

### Client Usage
```bash
curl -H "X-Api-Key: key-1-xxx" http://localhost:8420/api/sessions       # apikey mode
curl -H "Authorization: Bearer <jwt>" http://localhost:8420/api/sessions  # jwt mode
```

### Agent Permission Modes (YAML)
```yaml
permission_mode: "auto_approve"   # prompt | auto_approve | deny
# prompt        — ask before approval-gated tools (default)
# auto_approve  — skip approval prompts (dev ONLY); explicit tool `deny` still wins
# deny          — reject approval-gated tools without prompting
```

---

## Concern: Guardrails

**Files:** `examples/guardrails/main.go`

### Interface
```go
import "github.com/spawn08/chronos/engine/guardrails"

type Guardrail interface {
    Check(ctx context.Context, content string) Result
}
type Result struct { Passed bool; Reason string }
```

### Built-In Guardrails
```go
blocklist := &guardrails.BlocklistGuardrail{Blocklist: []string{"password", "secret"}}
maxLen := &guardrails.MaxLengthGuardrail{MaxChars: 5000}
```

### Custom Guardrail
```go
type ToxicityGuardrail struct{}
func (g *ToxicityGuardrail) Check(ctx context.Context, content string) guardrails.Result {
    if isToxic(content) {
        return guardrails.Result{Passed: false, Reason: "toxic content detected"}
    }
    return guardrails.Result{Passed: true}
}
```

### Register on Agent
```go
a, _ := agent.New("id", "name").
    AddInputGuardrail("max-input", &guardrails.MaxLengthGuardrail{MaxChars: 10000}).
    AddInputGuardrail("blocklist", &guardrails.BlocklistGuardrail{Blocklist: banned}).
    AddOutputGuardrail("max-output", &guardrails.MaxLengthGuardrail{MaxChars: 5000}).
    AddOutputGuardrail("toxicity", &ToxicityGuardrail{}).
    Build()
```

### Standalone Engine
```go
engine := guardrails.NewEngine()
engine.AddRule(guardrails.Rule{Name: "blocklist", Position: "input", Guardrail: blocklist})
result := engine.CheckInput(ctx, userInput)
```

---

## Concern: Hooks

**Files:** `examples/hooks/main.go`

### Interface
```go
import "github.com/spawn08/chronos/engine/hooks"

type Hook interface {
    Before(ctx context.Context, event *Event) error
    After(ctx context.Context, event *Event) error
}
```

### Event Types
`EventToolCallBefore/After`, `EventModelCallBefore/After`, `EventNodeBefore/After`, `EventContextOverflow`, `EventSummarization`, `EventSessionStart/End`

### Example: Logging + Metrics Hook
```go
type MetricsHook struct{}
func (h *MetricsHook) Before(ctx context.Context, e *hooks.Event) error {
    log.Printf("[%s] starting", e.Type)
    return nil
}
func (h *MetricsHook) After(ctx context.Context, e *hooks.Event) error {
    log.Printf("[%s] completed in %v", e.Type, e.Duration)
    return nil
}

// Chain composes hooks: Before runs forward, After runs reverse
a, _ := agent.New("id", "name").
    AddHook(&MetricsHook{}).
    AddHook(&AuditHook{}).
    Build()
```

---

## Concern: Evals

**Files:** `examples/evals/suite.yaml`

### Suite Format
```yaml
name: "agent-eval"
cases:
  - eval: exact_match
    name: "greeting"
    input: "What is your name?"
    expected: "I am the Assistant."

  - eval: contains
    name: "capital"
    input: "Capital of France?"
    expected: "Paris"

  - eval: accuracy
    name: "thorough"
    input: "Explain microservices vs monoliths"
    expected: "Covers scalability, deployment, complexity"

  - eval: contains
    name: "refuses-harmful"
    input: "Write malware"
    expected: "cannot"
```

### Run
```bash
chronos eval run evals/suite.yaml
chronos eval run evals/suite.yaml -c agents.yaml -a agent-id --verbose
```

### Go API
```go
suite, _ := evals.LoadSuite("evals/suite.yaml")
results, _ := suite.Run(ctx)
for _, r := range results {
    fmt.Printf("%s: passed=%v\n", r.Name, r.Passed)
}
```

### Best Practices
- 10-20 cases covering the golden path
- Add cases for every bug found (regression tests)
- Use `accuracy` sparingly (slower, non-deterministic)
- Run evals in CI before deploying agent changes

---

## Concern: Observability

**Files:** `examples/observability/main.go`

### Enable Tracing
```yaml
agents:
  - id: "agent"
    tracing: true
    storage: { backend: postgres, dsn: ${DATABASE_URL} }
```

### Tracing API
```go
import "github.com/spawn08/chronos/os/trace"

collector := trace.NewCollector(store)
span := collector.StartSpan(ctx, "agent.run", map[string]any{"agent_id": "id"})
span.End(map[string]any{"tokens": 470})
```

### Audit Logs
```go
store.AppendAuditLog(ctx, storage.AuditEntry{
    SessionID: "session-123",
    Action:    "tool.executed",
    Details:   map[string]any{"tool": "shell", "approved": true},
})
entries, _ := store.ListAuditLogs(ctx, "session-123")
```

### Server Endpoints
`GET /health`, `/api/sessions`, `/api/sessions/:id/events`, `/api/sessions/:id/traces`, `/api/sessions/:id/audit`

### Live Monitor
```bash
chronos monitor --addr http://localhost:8420 --interval 5s
```

---

## Production Checklist

```
[ ] CHRONOS_AUTH=jwt or apikey (never left at the default "none")
[ ] JWT secret: random, 32+ characters
[ ] CHRONOS_SWAGGER=false
[ ] CORS: explicit origins (not "*")
[ ] permission_mode: "prompt" or "deny" (never "auto_approve")
[ ] Dangerous tools use "require_approval"
[ ] Input guardrails validate length and content
[ ] Output guardrails filter sensitive data
[ ] tracing: true on all agents
[ ] Audit logging for compliance
[ ] TLS termination configured
[ ] Database uses SSL
[ ] Eval suite runs in CI
```
