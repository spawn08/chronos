# Chronos Skills for Claude Code

Portable, self-contained skills that help developers integrate the [Chronos](https://github.com/spawn08/chronos) AI agents framework into their projects using Claude Code. Each skill embeds the complete API reference and includes ready-to-copy example files — no Chronos repo context needed.

## Installation

### Install all skills (recommended)

```bash
# From your project root
mkdir -p .claude/skills
cp -r /path/to/chronos/skills/claude-code/chronos-* .claude/skills/
```

### One-liner (curl from repo)

```bash
mkdir -p .claude/skills && \
for skill in chronos-quickstart chronos-agents chronos-teams chronos-graph chronos-production chronos-deploy chronos-sdk; do
  mkdir -p .claude/skills/$skill
  curl -sL "https://raw.githubusercontent.com/spawn08/chronos/main/skills/claude-code/$skill/SKILL.md" \
    -o ".claude/skills/$skill/SKILL.md"
done
```

> The example files in each skill's `examples/` directory are for direct copying — install the full skill directory (`cp -r`) to get them.

## Skill Catalog

| Skill | What it covers | Example scenarios |
|-------|---------------|-------------------|
| **chronos-quickstart** | Getting started — add dep, first agent, run it | Hello world, bootstrapping |
| **chronos-agents** | All agent types with scenario-specific YAML + Go | Chatbot, code assistant, RAG, data pipeline, API/MCP, local/Ollama, support desk |
| **chronos-teams** | Multi-agent orchestration topologies | Sequential pipeline, router/triage, coordinator/boss-worker, swarm/handoff |
| **chronos-graph** | StateGraph workflow patterns | Linear, conditional branch, human-in-the-loop, retry loop |
| **chronos-production** | Production hardening concerns | Auth (JWT/API-key/RBAC), guardrails, hooks, evals, observability |
| **chronos-deploy** | Deployment targets | Docker Compose, Kubernetes, embed in existing Go service |
| **chronos-sdk** | Complete Go SDK API reference | Agent builder, memory, RAG, tools, MCP, streaming, providers, storage |

## Structure

Each skill follows this pattern:

```
chronos-agents/
  SKILL.md                          ← Claude Code reads this (API ref + scenario routing)
  examples/
    chatbot/agents.yaml             ← Ready-to-copy files for each scenario
    code-assistant/agents.yaml
    rag-agent/agents.yaml
    rag-agent/docker-compose.yaml
    data-pipeline/agents.yaml
    api-agent/agents.yaml
    local-ollama/agents.yaml
    support-desk/agents.yaml
```

- **SKILL.md** is what Claude Code reads — it contains the API reference, scenario templates, and routing logic
- **examples/** contain runnable artifacts developers copy directly into their project

## How It Works

When you ask Claude Code to help with Chronos:

```
You: "Create a customer support system with agent routing"

Claude Code activates: chronos-agents (support-desk scenario) + chronos-teams (router topology)
→ Copies examples/support-desk/agents.yaml as a starting point
→ Customizes the agent system prompts for your domain
→ Shows CLI commands to run and test
```

```
You: "Set up a RAG agent that searches our documentation"

Claude Code activates: chronos-agents (rag-agent scenario)
→ Copies examples/rag-agent/agents.yaml + docker-compose.yaml
→ Generates loader.go for document ingestion
→ Wires up vector store and embeddings provider
```

## Requirements

- Go 1.24+
- Claude Code CLI, desktop app, or IDE extension
- `go get github.com/spawn08/chronos` in your project
