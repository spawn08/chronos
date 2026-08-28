---
title: "Durability & Sandboxing"
---


# Durability & Sandboxing

The runtime-first features you only discover you need after you ship: durable work that survives crashes, human-in-the-loop resume, and sandboxed execution of untrusted commands. All run with **no API key**.

```bash
go run ./examples/<name>/
```

---

## durable_queue

Durable work queue with leased workers, durable sleep, park/signal human-in-the-loop, and orphan recovery — so a crashed worker's job is picked up and finished, not lost.

```bash
go run ./examples/durable_queue/
```

**Demonstrates:**
- Leased workers with heartbeats and lease TTLs
- `Sleep` — durable reschedule after a delay without holding a goroutine
- Park/signal pattern for human-in-the-loop pauses
- Orphan recovery: reclaiming runs whose worker died mid-lease

Source: [examples/durable_queue](https://github.com/spawn08/chronos/tree/main/examples/durable_queue)

---

## durable_hitl

Human-in-the-loop approval with checkpoint and resume: the graph pauses at an interrupt node, persists a checkpoint, and resumes from exactly that point once a human approves.

```bash
go run ./examples/durable_hitl/
```

**Demonstrates:**
- `AddInterruptNode` pausing execution with a durable checkpoint
- Resuming a run from its last checkpoint after an out-of-band approval
- State survives process restarts

Source: [examples/durable_hitl](https://github.com/spawn08/chronos/tree/main/examples/durable_hitl)

---

## durable_llm_graph

Shows **where LLM calls happen and how the StateGraph runtime makes them durable**: a 3-step content pipeline (`draft` (LLM) → `review` (LLM) → `finalize`) where `review` is made to fail on its first attempt to simulate a crash. On `Resume`, `draft`'s already-checkpointed output is skipped and execution picks up at `review` — so a completed, expensive LLM call is never re-executed. Uses a deterministic stub provider so it runs offline and in CI with no API keys.

```bash
go run ./examples/durable_llm_graph/
```

**Demonstrates:**
- LLM calls inside graph nodes, with the runtime itself remaining LLM-agnostic
- Checkpointing after every completed node
- `Resume` continuing from the last checkpoint after a simulated crash, without re-running expensive prior nodes

Source: [examples/durable_llm_graph](https://github.com/spawn08/chronos/tree/main/examples/durable_llm_graph)

---

## sandbox_execution

Process sandbox for running untrusted commands with timeouts and output capture.

```bash
go run ./examples/sandbox_execution/
```

**Demonstrates:**
- `sandbox.NewProcessSandbox(workDir)` — isolated execution environment
- Stdout/stderr capture
- Exit code handling
- Timeout enforcement (10s command killed after 500ms)
- File I/O within the sandbox working directory
- Environment variable access

:::note
Beyond the process backend shown here, Chronos also ships container, Kubernetes-job, and WASM sandbox backends — see the [sandbox package](https://github.com/spawn08/chronos/tree/main/sandbox).
:::
