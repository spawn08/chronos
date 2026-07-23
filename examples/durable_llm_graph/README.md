# durable_llm_graph

Shows **where LLM calls happen** in Chronos and **how the StateGraph runtime makes them durable**.

The graph is a 3-step pipeline where the LLM is called inside nodes:

```
draft (LLM) ──▶ review (LLM) ──▶ finalize
```

`review` is rigged to fail on its first attempt to simulate a crash *after* the
expensive `draft` LLM call has completed. The runner checkpoints state after
every completed node, so on **Resume**:

- `draft` is **not** re-executed (its output was checkpointed) — its LLM call
  does not repeat, and its `🔵 [draft]` log line does not print again.
- execution picks up at `review`, then finishes at `finalize`.

Key point: `engine/graph` never calls an LLM itself — it only orchestrates and
checkpoints. LLM calls live in the node functions (`sdk/agent` does the same one
layer up). The example uses a deterministic offline stub `model.Provider`; swap
in OpenAI/Anthropic/Gemini/Ollama and the graph code is unchanged.

## Run

```bash
go run ./examples/durable_llm_graph/
go test ./examples/durable_llm_graph/
```

See also: `examples/graph_with_llm` (real providers + tools) and
`examples/durable_hitl` (interrupt-node human approval).
