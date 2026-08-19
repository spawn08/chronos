---
name: chronos-agents
description: Create Chronos AI agents for any scenario — chatbot, code assistant, RAG/knowledge, data pipeline, API integration, local/Ollama, customer support, and skills-enabled agents. Each scenario has ready-to-copy YAML + Go files in examples/.
---

# Chronos Agent Scenarios

## Activation
Use this skill when:
- Creating any Chronos agent (YAML or Go)
- Developer describes a use case and needs the right agent configuration
- Generating agents.yaml, main.go, or supporting files for a Chronos project
- Defining or referencing skills in YAML agent configs

## Scenario Selection

| Scenario | Directory | When to use |
|----------|-----------|-------------|
| **Chatbot** | `examples/chatbot/` | Conversational assistant, FAQ bot, helpdesk |
| **Code Assistant** | `examples/code-assistant/` | Read/write files, run commands, debug code |
| **RAG Agent** | `examples/rag-agent/` | Answer questions from documents/knowledge base |
| **Data Pipeline** | `examples/data-pipeline/` | Structured output, ETL, analysis, extraction |
| **API Agent** | `examples/api-agent/` | External APIs via MCP servers, webhooks |
| **Local/Ollama** | `examples/local-ollama/` | Offline, self-hosted models, no cloud API |
| **Support Desk** | `examples/support-desk/` | Customer support with agent routing |
| **Skills Catalog** | `examples/skills-catalog/` | Agents with reusable skills (catalog + inline) |
| **Production Agent** | `examples/production-agent/` | Full-stack: memory, RAG, guardrails, security, sandbox, observability |

Copy the relevant `examples/<scenario>/` directory into the developer's project and customize.

---

## Scenario: Chatbot

**Files:** `examples/chatbot/agents.yaml`, `examples/chatbot/main.go`

Key config:
```yaml
agents:
  - id: "chatbot"
    name: "Chatbot"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      You are a friendly assistant for [COMPANY].
      Be concise. Ask clarifying questions when ambiguous.
      Redirect off-topic questions politely.
    num_history_runs: 10
    stream: true
    context:
      max_tokens: 8192
      summarize_threshold: 6000
      preserve_recent_turns: 4
```

Run: `chronos agent chat -c agents.yaml -a chatbot`

---

## Scenario: Code Assistant

**Files:** `examples/code-assistant/agents.yaml`

Key config — uses built-in file and shell tools with permission controls:
```yaml
agents:
  - id: "coder"
    name: "Code Assistant"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      You are an expert software engineer.
      Read existing code before making changes. Run tests after.
    tools:
      - { name: "file_read", permission: "allow" }
      - { name: "file_list", permission: "allow" }
      - { name: "file_glob", permission: "allow" }
      - { name: "file_grep", permission: "allow" }
      - { name: "file_write", permission: "require_approval" }
      - { name: "shell", permission: "require_approval" }
    permission_mode: "prompt"
    reasoning: { strategy: "cot", effort: "high" }
    context: { max_tokens: 16384 }
    stream: true
```

For sandboxed execution (CI/automated):
```yaml
    tools:
      - { name: "shell_auto", permission: "allow" }
      - { name: "file_read", permission: "allow" }
      - { name: "file_write", permission: "allow" }
    permission_mode: "auto"
deployment:
  sandbox: { backend: "container", image: "golang:1.24-alpine", network: "none", timeout: "15m" }
```

---

## Scenario: RAG Agent

**Files:** `examples/rag-agent/agents.yaml`, `examples/rag-agent/loader.go`, `examples/rag-agent/docker-compose.yaml`

Requires a vector store (Qdrant, pgvector, Pinecone, etc.) and an embeddings provider.

Key config:
```yaml
agents:
  - id: "knowledge-agent"
    name: "Knowledge Agent"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      Answer questions using ONLY the retrieved documents.
      Always cite which document your answer comes from.
      If the context doesn't contain the answer, say so.
    tools:
      - name: "search_knowledge"
        description: "Search the knowledge base"
        parameters:
          type: object
          properties:
            query: { type: string, description: "Search query" }
            top_k: { type: integer, description: "Results count (default 5)" }
          required: ["query"]
        permission: "allow"
    stream: true
```

Go setup with VectorKnowledge:
```go
kb := knowledge.NewVectorKnowledge("docs", 1536, vectorStore, embedder,
    "text-embedding-3-small",
    knowledge.WithTopK(5),
    knowledge.WithScoreThreshold(0.7),
    knowledge.WithChunking(512, 64),
)
```

Vector store options: `qdrant.New(url)`, `pgvector.New(db)`, `chromadb.New(url)`, `pinecone.New(host, apiKey)`, `lancedb.New(url, apiKey, db)`, `milvus.New(endpoint, token)`, `weaviate.New(endpoint, apiKey)`, `redisvector.New(addr)`

---

## Scenario: Data Pipeline

**Files:** `examples/data-pipeline/agents.yaml`

Key config — uses structured output schema:
```yaml
agents:
  - id: "extractor"
    name: "Data Extractor"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: |
      Extract structured data from the input.
      Follow the output schema exactly. Never add fields not in the schema.
    output_schema:
      type: object
      properties:
        entities:
          type: array
          items:
            type: object
            properties:
              name: { type: string }
              type: { type: string, enum: ["person", "org", "location"] }
              confidence: { type: number }
            required: ["name", "type"]
        summary: { type: string }
        sentiment: { type: string, enum: ["positive", "negative", "neutral"] }
      required: ["entities", "summary", "sentiment"]
    reasoning: { strategy: "cot", effort: "medium" }
```

---

## Scenario: API Agent (MCP)

**Files:** `examples/api-agent/agents.yaml`

Key config — connects to external tools via MCP servers:
```yaml
agents:
  - id: "api-agent"
    name: "API Agent"
    model:
      provider: anthropic
      model: claude-sonnet-4-20250514
      api_key: ${ANTHROPIC_API_KEY}
    system_prompt: "You help users by calling external APIs and tools."
    mcp_servers:
      - name: "github"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-github"]
        permission: "require_approval"
      - name: "postgres"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-postgres", "${DATABASE_URL}"]
        permission: "require_approval"
      - name: "brave-search"
        transport: "stdio"
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-brave-search"]
        permission: "allow"
      - name: "custom-api"
        transport: "sse"
        url: "https://mcp.example.com/sse"
        permission: "allow"
    stream: true
```

MCP transports: `stdio` (spawns subprocess) or `sse` (connects to HTTP endpoint).

Common MCP packages: `@modelcontextprotocol/server-filesystem`, `server-github`, `server-postgres`, `server-brave-search`, `server-memory`, `server-puppeteer`, `server-slack`.

---

## Scenario: Local/Ollama

**Files:** `examples/local-ollama/agents.yaml`

Key config — no cloud API needed:
```yaml
agents:
  - id: "local-agent"
    name: "Local Agent"
    model:
      provider: ollama
      model: llama3.1
      base_url: "http://localhost:11434"
      timeout_sec: 300
    storage:
      backend: sqlite
      dsn: "local.db"
    system_prompt: "You are a helpful local assistant."
    stream: true
    reasoning: { strategy: "none" }
```

Run Ollama: `ollama serve` then `ollama pull llama3.1`

---

## Scenario: Support Desk

**Files:** `examples/support-desk/agents.yaml`

Key config — multiple specialized agents with a router:
```yaml
agents:
  - id: "billing"
    name: "Billing Agent"
    description: "Handles billing, payments, and invoicing"
    capabilities: ["billing", "payments", "invoicing"]
    system_prompt: "You handle billing questions."

  - id: "technical"
    name: "Technical Agent"
    description: "Handles technical issues and troubleshooting"
    capabilities: ["debugging", "troubleshooting", "technical_support"]
    system_prompt: "You handle technical support."

  - id: "general"
    name: "General Agent"
    description: "Handles general inquiries"
    capabilities: ["faq", "general_info"]
    system_prompt: "You handle general questions."

teams:
  - id: "support"
    name: "Support Desk"
    strategy: "router"
    router: "capability"
    agents: ["billing", "technical", "general"]
    error_strategy: "fail_fast"
```

Run: `chronos team run -c agents.yaml -t support -m "I need help with my invoice"`

---

## Scenario: Skills Catalog

**Files:** `examples/skills-catalog/agents.yaml`, `examples/skills-catalog/skills/*/SKILL.md`

Skills inject knowledge into the agent's system prompt and declare which tools they rely on. They are **descriptive, not executable** — tools must still be registered separately. Two mechanisms:

### Mechanism 1: `skills_dir` + `use_skills` (catalog)

Create `SKILL.md` files in a directory, reference them by name:

```
skills/
  summarize/SKILL.md
  code-review/SKILL.md
  data-analyst/SKILL.md
```

Each `SKILL.md` uses frontmatter + markdown body:

```markdown
---
name: summarize
version: 1.0.0
description: Summarize documents concisely.
author: team-name
tags: [nlp, summarization]
tools: [file_read]
---

# Summarization Skill

## When to use
Activate when the user asks to summarize documents...

## Approach
1. Read the full content first
2. Identify main themes and key points
3. Produce a structured summary
```

Reference in `agents.yaml`:

```yaml
skills_dir: skills             # path to directory of <name>/SKILL.md files

agents:
  - id: "researcher"
    name: "Research Agent"
    use_skills:                # reference by name from skills_dir
      - summarize
      - data-analyst
    tools:
      - { name: "file_read", permission: "allow" }
```

The SKILL.md markdown body is injected verbatim into the agent's system prompt under an `## Available skills` section.

### Mechanism 2: Inline `skills` (no files needed)

Define skills directly in the YAML:

```yaml
agents:
  - id: "reviewer"
    name: "Code Reviewer"
    skills:
      - name: "security-review"
        version: "1.0"
        description: "Review code for security vulnerabilities"
        author: "security-team"
        tags: ["security", "code-review"]
        tools: ["file_read", "file_grep"]

      - name: "performance-review"
        version: "1.0"
        description: "Review code for performance issues"
        tools: ["file_read", "shell"]
```

### Mechanism 3: `manifest_path` (inline + file body)

Point an inline skill to a markdown file for detailed instructions:

```yaml
skills:
  - name: "project-conventions"
    version: "1.0"
    description: "Project-specific coding conventions"
    manifest_path: "skills/conventions.md"   # body read from file
```

### SkillConfig Fields

| Field | YAML Tag | Type | Purpose |
|-------|----------|------|---------|
| `name` | `name` | string | Skill identifier (required) |
| `version` | `version` | string | Semantic version |
| `description` | `description` | string | What the skill does |
| `author` | `author` | string | Who created it |
| `tags` | `tags` | []string | Categorization |
| `tools` | `tools` | []string | Tool names this skill relies on (metadata only) |
| `manifest` | `manifest` | map | Arbitrary key-value metadata |
| `manifest_path` | `manifest_path` | string | Path to markdown file (body injected into prompt) |

### Combining Both

```yaml
skills_dir: skills

agents:
  - id: "full-agent"
    use_skills: ["summarize", "code-review"]   # from catalog
    skills:                                      # plus inline
      - name: "project-conventions"
        manifest_path: "skills/conventions.md"
```

Run: `chronos agent chat -c agents.yaml -a full-agent`

---

## Scenario: Production Agent

**Files:** `examples/production-agent/agents.yaml`, `examples/production-agent/main.go`, `examples/production-agent/docker-compose.yaml`, `examples/production-agent/skills/*/SKILL.md`

The complete production-grade agent wiring every concern together. Copy the entire `examples/production-agent/` directory.

### What's included

| Concern | How it's configured | Where |
|---------|-------------------|-------|
| **Memory** | `memory.Manager` with vector recall | `main.go` (programmatic) |
| **RAG / Knowledge** | `VectorKnowledge` with Qdrant + OpenAI embeddings | `main.go` (programmatic) |
| **Durable storage** | PostgreSQL with connection pooling | `agents.yaml` (storage block) |
| **Vector database** | Qdrant for both RAG and memory recall | `docker-compose.yaml` |
| **Guardrails** | Input: max-length + blocklist. Output: max-length + no-secrets | `main.go` (programmatic) |
| **Security** | `permission_mode: "prompt"`, tool-level permissions, approval flow | `agents.yaml` |
| **Sandbox** | Container-based with network and timeout limits | `agents.yaml` (deployment block) |
| **Observability** | Tracing spans, audit logs, `tracing: true` | `agents.yaml` + `main.go` |
| **Skills** | 6 operational skills injected into system prompt | `skills/*/SKILL.md` |
| **MCP** | PostgreSQL MCP server for database queries | `agents.yaml` (mcp_servers) |

### Skills catalog

The `skills/` directory contains 6 production-grade skills that inject operational knowledge:

| Skill | What it teaches the agent |
|-------|--------------------------|
| `memory-architecture` | Three memory layers, when to remember/recall/forget |
| `security-ops` | Permission model, approval workflow, security rules |
| `data-persistence` | PostgreSQL + Qdrant architecture, query patterns |
| `observability-ops` | Tracing, audit logging, monitoring endpoints |
| `guardrails-ops` | Input/output validation rules, what happens when blocked |
| `sandbox-ops` | Container boundaries, filesystem, network limits |

### Why skills are separate files

Skills are **descriptive** — they inject knowledge into the system prompt. They don't configure runtime behavior (that's done in YAML + Go). But giving the agent operational knowledge about its own architecture makes it:
- Self-aware of its security boundaries (won't try to bypass them)
- Effective at using memory tools (knows when to remember vs. recall)
- Transparent about its limitations (knows what the sandbox restricts)
- Properly grounded (knows to cite sources from knowledge search)

### Current limitation

`manifest_path` accepts a **single file** per skill. To compose detailed knowledge from multiple sources, use multiple skills (each with its own SKILL.md) rather than multiple files per skill. The `use_skills` list is the composition mechanism.

### Run

```bash
# Start infrastructure
docker compose -f examples/production-agent/docker-compose.yaml up -d

# Set environment
export DATABASE_URL="postgres://chronos:password@localhost:5432/chronos"
export QDRANT_URL="http://localhost:6333"
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."

# Run programmatically
go run examples/production-agent/main.go

# Or via CLI (YAML-only features, no programmatic memory/RAG)
chronos agent chat -c examples/production-agent/agents.yaml -a production-agent
```

---

## YAML Field Reference

### Model Providers
`openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`

### Model Config
```yaml
model:
  provider: ""           # required
  model: ""              # required — model ID
  api_key: ""            # ${ENV_VAR} expansion supported
  base_url: ""           # override endpoint (ollama, compatible)
  org_id: ""             # OpenAI org
  timeout_sec: 120
  endpoint: ""           # Azure only
  deployment: ""         # Azure only
  api_version: ""        # Azure only
```

### Storage Config
```yaml
storage:
  backend: "sqlite"      # sqlite | postgres | none
  dsn: "agent.db"
  max_open_conns: 10
  max_idle_conns: 5
  conn_max_lifetime_sec: 300
```

### Tool Config
```yaml
tools:
  - name: ""             # snake_case
    description: ""
    parameters: {}        # JSON Schema
    permission: "allow"   # allow | require_approval | deny
```

### Built-in Tools
`shell`, `shell_auto`, `file_read`, `file_write`, `file_list`, `file_glob`, `file_grep`

### CLI Commands
```bash
chronos run -c agents.yaml -a <id> -m "message"
chronos agent chat -c agents.yaml -a <id>
chronos agent list -c agents.yaml
chronos agent info -c agents.yaml -a <id>
chronos deploy agents.yaml "task"
chronos serve :8420
```
