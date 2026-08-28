---
title: "LLM Agents"
---


# LLM Agents

Examples that make real LLM API calls. Each resolves its provider from the environment — set any of the key sets in [Choosing a provider](../examples.md#choosing-a-provider) and the commands below are identical across OpenAI, Azure OpenAI, Vertex AI, Bedrock, and the rest. Most fall back to a mock provider when no key is set.

---

## graph_with_llm

**StateGraph with real LLM calls inside nodes.** This is the most important example for understanding how Chronos combines graph workflows with live LLM reasoning. A classifier node calls the LLM to categorize questions, then conditional edges route to a technical expert (with tools) or a general assistant.

```bash
# OpenAI
OPENAI_API_KEY=sk-... go run ./examples/graph_with_llm/

# Azure OpenAI
AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com \
  AZURE_OPENAI_DEPLOYMENT=gpt-4o go run ./examples/graph_with_llm/

# Google Cloud Vertex AI
GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token) \
  go run ./examples/graph_with_llm/

# AWS Bedrock
AWS_REGION=us-east-1 AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
  go run ./examples/graph_with_llm/
```

With no provider configured, this example falls back to a local Ollama server at `localhost:11434`.

**Demonstrates:**
- Wiring real LLM providers (OpenAI, Anthropic, Gemini, Azure OpenAI, Vertex AI, Bedrock, Ollama) into graph nodes
- Conditional routing based on LLM classification output
- Tool calling within graph nodes
- Checkpointing with SQLite
- The YAML equivalent (see `examples/yaml-configs/graph-agent.yaml`)

See the [Building Real-World Agents](/guides/real-world-agents/) guide for a detailed walkthrough.

---

## mcp_agent

**Model Context Protocol integration.** Connects to an MCP server over stdio, imports every tool it advertises into the agent's registry, and lets the model call them. Uses the official filesystem MCP server.

```bash
# Optional live tool server:
npm install -g @modelcontextprotocol/server-filesystem

# Any provider works — the model that calls the MCP tools is picked from env:
OPENAI_API_KEY=sk-... go run ./examples/mcp_agent/

# Azure OpenAI
AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com \
  AZURE_OPENAI_DEPLOYMENT=gpt-4o go run ./examples/mcp_agent/

# Google Cloud Vertex AI
GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token) \
  go run ./examples/mcp_agent/
```

With no provider set, the tools are still imported and listed — only the model call is skipped.

**Demonstrates:**
- `AddMCPServer(mcp.ServerConfig{...})` — register an MCP server on the agent
- `ConnectMCP(ctx)` — launch servers and import their tools
- Inspecting imported tools via `agent.Tools.List()`
- Graceful degradation when the server binary is absent
- Any provider (OpenAI, Azure OpenAI, Vertex AI, Bedrock, …) driving the MCP tool calls

See the [Model Context Protocol guide](/guides/mcp) for the full workflow.

---

## mcp_sse

**MCP over the HTTP+SSE transport.** Unlike `mcp_agent` (which launches a stdio subprocess), this connects to a remote-style MCP server over HTTP: the client opens a long-lived SSE stream for responses and POSTs JSON-RPC requests. To stay self-contained and CI-safe, the example starts a minimal in-process SSE MCP server, connects to it, lists its tools, and calls one — no API key required.

```bash
go run ./examples/mcp_sse/
```

**Demonstrates:**
- MCP 2024-11-05 HTTP+SSE transport (client and a minimal server)
- Listing and calling tools advertised over SSE
- The same tool-import pattern as `mcp_agent`, over a different transport

See the [Model Context Protocol guide](/guides/mcp) for stdio vs. HTTP+SSE transport details.

---

## coding_agent

**Autonomous, Cursor/Aider-style coding agent.** Reads, writes, and searches files, runs shell commands (git, build, tests), and uses a vector store for semantic code search (RAG). Runs an autonomous multi-step loop.

```bash
# OpenAI
OPENAI_API_KEY=sk-... go run ./examples/coding_agent/

# Azure OpenAI
AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com \
  AZURE_OPENAI_DEPLOYMENT=gpt-4o go run ./examples/coding_agent/

# Google Cloud Vertex AI
GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token) \
  go run ./examples/coding_agent/
```

**Demonstrates:**
- Wiring built-in file tools + shell tools + custom tools onto one agent
- `VectorKnowledge` with in-memory embeddings for code search (RAG)
- An autonomous agent loop bounded by `MaxIterations`
- Combining tools with system prompts for effective coding workflows
- Any provider (OpenAI, Azure OpenAI, Vertex AI, Bedrock, …); mock fallback when nothing is set

---

## multi_agent

All 4 team strategies (sequential, parallel, router, coordinator), direct channels, and bus delegation. Works with a mock provider if no API key is set.

```bash
# With mock (no key)
go run ./examples/multi_agent/

# With OpenAI
OPENAI_API_KEY=sk-... go run ./examples/multi_agent/

# With Azure OpenAI
AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com \
  AZURE_OPENAI_DEPLOYMENT=gpt-4o go run ./examples/multi_agent/

# With Google Cloud Vertex AI
GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token) \
  go run ./examples/multi_agent/
```

**Demonstrates:**
- Sequential, parallel, router, and coordinator team strategies
- Direct agent-to-agent channels and inter-agent bus delegation
- Any provider (OpenAI, Azure OpenAI, Vertex AI, Bedrock, …); mock fallback when nothing is set

---

## team_deploy

**Deploy multi-agent teams from YAML with sandbox isolation.** Loads a team deployment config, builds agents with YAML-defined tools, and runs them in a sandboxed process environment using sequential and coordinator strategies.

```bash
# OpenAI
OPENAI_API_KEY=sk-... go run ./examples/team_deploy/

# Azure OpenAI
AZURE_OPENAI_API_KEY=... AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com \
  AZURE_OPENAI_DEPLOYMENT=gpt-4o go run ./examples/team_deploy/

# Google Cloud Vertex AI
GOOGLE_CLOUD_PROJECT=my-project GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token) \
  go run ./examples/team_deploy/

# Or via the CLI:
chronos deploy examples/team_deploy/deploy.yaml "Add error handling to the API"
```

**Demonstrates:**
- YAML-driven agent and team configuration
- Sandbox-backed tool execution for safe agent autonomy
- Sequential pipeline vs. coordinator strategies
- Deploying a full coding team from a single config file
- Any provider (OpenAI, Azure OpenAI, Vertex AI, Bedrock, …); mock fallback when nothing is set
