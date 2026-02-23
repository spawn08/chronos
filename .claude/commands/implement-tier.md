Implement a specific tier of the Chronos development roadmap.

The tier number is: $ARGUMENTS

## Tier Reference (from DEVELOPMENT.md)

- **Tier 1** — Critical agent wiring: Knowledge.Search, MemoryManager.GetUserMemories/ExtractMemories, OutputSchema, NumHistoryRuns, output guardrails, tool/model hooks in Chat and handleToolCalls. See sdk/agent/agent.go.
- **Tier 2** — CLI expansion: run (headless), sessions list|resume|export, skills list|add|remove|create, kb add|list|search|clear, memory list|forget|clear, mcp list|add|remove|tools, config show|set|model, db init|status|backup, monitor (TUI). REPL: agent integration, slash commands, shell escape, multi-line.
- **Tier 3** — Storage adapter stubs: implement full Storage or VectorStore for dynamo, mongo, redis, redisvector, pinecone, weaviate, milvus. Use /new-adapter for scaffolding.
- **Tier 4** — Embedding providers: implement model.EmbeddingsProvider for OpenAI, Anthropic/Voyage, Cohere, Ollama, or HuggingFace. Use /add-embedding-provider for scaffolding.
- **Tier 5** — ChronosOS: sessions API, traces API, auth middleware, dashboard package, Runner→Broker event publishing, trace collector wiring.
- **Tier 6** — Scalable sandbox: container-based sandbox (Docker/gVisor), resource limits, pooling, network isolation, K8s Job execution. Use /scale-sandbox.
- **Tier 7** — Production hardening: Helm Secret/Ingress/HPA/ServiceAccount/PDB, migration framework, MCP client, evals suite, built-in skill tools, tests.

## Instructions

1. Parse $ARGUMENTS as a single tier number (1–7). If not a number or out of range, list all tiers and ask which to implement.
2. Open DEVELOPMENT.md and the relevant source files for that tier.
3. Implement the items for that tier in dependency order. Prefer small, verifiable steps.
4. After each logical unit, run `go build ./...` and fix any errors.
5. Do not implement multiple tiers in one run unless the user explicitly asks (e.g. "implement tier 1 and 2").
6. Summarize what was implemented and what (if anything) remains for that tier.
