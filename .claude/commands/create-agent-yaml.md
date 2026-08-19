Scaffold a complete YAML agent configuration for Chronos.

The agent description is: $ARGUMENTS

## Instructions

1. Create an `agents.yaml` file (or add to an existing one) with a full agent definition based on the description provided.

2. Ask the user which model provider to use. Supported providers:
   - `openai`, `anthropic`, `gemini`, `mistral`, `ollama`, `azure`, `groq`, `together`, `deepseek`, `openrouter`, `fireworks`, `perplexity`, `anyscale`, `compatible`

3. Generate the YAML using this complete schema reference:

```yaml
defaults:
  model:
    provider: anthropic
    model: claude-sonnet-4-20250514
    api_key: ${ANTHROPIC_API_KEY}
  storage:
    backend: sqlite
    dsn: "chronos.db"

agents:
  - id: "agent-id"
    name: "Agent Name"
    description: "What this agent does"
    user_id: "default"

    model:
      provider: "anthropic"
      model: "claude-sonnet-4-20250514"
      api_key: "${ANTHROPIC_API_KEY}"
      base_url: ""           # override for compatible/ollama
      org_id: ""             # OpenAI org
      timeout_sec: 120
      # Azure-specific:
      # endpoint: "https://xxx.openai.azure.com"
      # deployment: "my-deployment"
      # api_version: "2024-02-01"

    storage:
      backend: "sqlite"     # sqlite | postgres | none
      dsn: "agent.db"
      max_open_conns: 10
      max_idle_conns: 5
      conn_max_lifetime_sec: 300

    system_prompt: |
      You are a helpful assistant specialized in...

    instructions:
      - "Always cite sources"
      - "Respond concisely"

    capabilities:
      - "code_generation"
      - "data_analysis"

    tools:
      - name: "search_web"
        description: "Search the web for information"
        parameters:
          type: object
          properties:
            query:
              type: string
              description: "Search query"
          required: ["query"]
        permission: "allow"   # allow | require_approval | deny

    mcp_servers:
      - name: "filesystem"
        transport: "stdio"    # stdio | sse
        command: "npx"
        args: ["-y", "@modelcontextprotocol/server-filesystem", "/data"]
        permission: "allow"

    skills:
      - name: "summarize"
        version: "1.0"
        description: "Summarize documents"

    use_skills: ["summarize"]

    output_schema:
      type: object
      properties:
        answer:
          type: string
        confidence:
          type: number
      required: ["answer"]

    reasoning:
      strategy: "cot"        # none | cot | reflection
      native: false
      effort: "medium"       # low | medium | high
      budget_tokens: 4096
      summary: true

    context:
      max_tokens: 8192
      summarize_threshold: 6000
      preserve_recent_turns: 4

    num_history_runs: 5
    stream: true
    debug: false
    tracing: true
    permission_mode: "prompt"  # prompt | auto | strict
    sub_agents: []

deployment:
  name: "my-agent"
  sandbox:
    backend: "process"       # process | container | k8s
    work_dir: "/app"
    image: "chronos-agent:latest"
    network: "bridge"
    timeout: "5m"
```

4. Tailor the generated YAML to the user's description:
   - For **conversational agents**: focus on system_prompt, instructions, stream: true
   - For **tool-using agents**: add relevant tool definitions with JSON Schema parameters
   - For **RAG agents**: add knowledge tools (file_read, file_grep) and suggest setting up VectorKnowledge
   - For **code agents**: add shell, file_read, file_write, file_list tools with appropriate permissions
   - For **autonomous agents**: set permission_mode to "auto", add guardrail tools
   - For **structured output agents**: define output_schema

5. Include environment variable placeholders for API keys: `${PROVIDER_API_KEY}`

6. Show how to run the agent:
```bash
# Run with a single message
go run ./cli/main.go run -c agents.yaml -a agent-id -m "Hello"

# Interactive REPL
go run ./cli/main.go agent chat -c agents.yaml -a agent-id

# Deploy in sandbox
go run ./cli/main.go deploy agents.yaml "Start working"

# Serve as HTTP API
go run ./cli/main.go serve :8420
```

7. Verify the YAML is valid: `go run ./cli/main.go agent list -c agents.yaml`
