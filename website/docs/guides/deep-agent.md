---
title: "Deep Agent Preset"
sidebar_label: "Deep Agent"
---


The **deep agent** preset is the batteries-included capstone of the Chronos agent
harness. A single `harness.NewDeepAgent(...)` call assembles every harness
primitive into one ready-to-run agent, with a sensible default prompt and tool
set and no manual wiring:

| Capability | Primitive | What it gives the agent |
|------------|-----------|-------------------------|
| **Planning** | [`update_plan`](planning.md) (WC-A-001) | A revisable task list it maintains across turns |
| **Context offloading** | [virtual filesystem](virtual-filesystem.md) (WC-A-002) | `fs_write`/`fs_read`/`fs_ls`/`fs_delete` scratch space |
| **Delegation** | [context-isolated subagents](subagents.md) (WC-A-003) | `spawn_subagent` — sub-tasks in a fresh context |
| **Compaction** | [automatic context management](context-management.md) (WC-A-004) | Older turns summarized; the active plan **pinned** |
| **Memory** | [semantic recall](semantic-recall.md) (WC-D-001) | Cross-session long-term recall (when a manager is attached) |

## Quick start

```go
import (
    "github.com/spawn08/chronos/sdk/harness"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

store, _ := sqlite.New("agent.db")
_ = store.Migrate(ctx)

a, err := harness.NewDeepAgent(harness.DeepAgentConfig{
    Model:   provider,   // required
    Storage: store,      // durable plan + files + session compaction
})
if err != nil {
    log.Fatal(err)
}

// Use ChatWithSession for the full durable, self-compacting experience.
resp, _ := a.ChatWithSession(ctx, "task-1", "Research X and write a report.")
```

That is all the wiring required. The returned value is a normal `*agent.Agent`, so
everything else on the agent (streaming, hooks, guardrails, teams) still applies.

## Configuration

`DeepAgentConfig` is opinionated but fully override-able. Only `Model` is required.

| Field | Default | Purpose |
|-------|---------|---------|
| `Model` | — (required) | The LLM provider driving the loop |
| `ID` / `Name` | `deep-agent` / `Deep Agent` | Agent identity |
| `Storage` | nil → in-memory | Durable plan + VFS + session compaction. Must implement `storage.SessionFileStore` (sqlite, postgres) |
| `MemoryManager` | nil | Attach for cross-session semantic recall |
| `Broker` | nil | Receives plan-update stream events |
| `SystemPrompt` | `DefaultDeepAgentSystemPrompt` | Override the default deep-agent prompt |
| `Instructions` | none | Extra system-level guidance |
| `SubAgents` | none | Pre-registered specialist templates |
| `MaxSubAgentDepth` | 3 | Bound on subagent nesting |
| `DisableSubAgents` | false | Omit `spawn_subagent` entirely |
| `SubAgentRunner` | in-process | Pass a `QueuedRunner` for durable, relocatable subagents |
| `ExtraTools` / `ExtraToolkits` | none | Add domain tools (web search, SQL, …) |
| `Context` | 0.8 threshold, keep 6 turns | Compaction policy |

### Storage and compaction

With a `Storage` backend the plan and the virtual filesystem are durable and
`ChatWithSession` compacts the conversation automatically as it approaches the
model's context window. Without storage, the plan and VFS are in-memory
(ephemeral) and only `Chat` is available (single-turn, no compaction).

The active plan is **pinned** into the system context every turn via the
`WithContextPins` seam, so summarization never drops it — the agent always sees
its current checklist even after older turns are compacted away. See
[Context Management](context-management.md#automatic-compaction--pinned-context).

### Pre-registered subagents

Register named specialists the agent can select by name (it can also invent
subagents dynamically at runtime):

```go
a, _ := harness.NewDeepAgent(harness.DeepAgentConfig{
    Model:   provider,
    Storage: store,
    SubAgents: []harness.SubAgentSpec{{
        Name:         "researcher",
        Description:  "Researches a topic and returns a concise finding.",
        SystemPrompt: "You are a focused researcher. Answer in one paragraph.",
        ToolNames:    []string{"web_search"}, // a subset of the parent's tools
    }},
})
```

A subagent runs in its own fresh conversation and returns only its final result,
so its intermediate reasoning never enters the parent's context window.

## How it fits together

`NewDeepAgent` builds the parent agent with the planning and VFS toolkits, the
compaction policy, and the plan pin, then derives the subagent service from the
built agent and attaches `spawn_subagent`. This ordering is why the SDK stays
decoupled from the built-in tools: the harness package (not `sdk/agent`) owns the
assembly, and the plan is pinned through the generic `WithContextPins` seam rather
than a hard dependency on the planning toolkit.

## Complete example

See [`examples/deep_agent/`](https://github.com/spawn08/chronos/tree/main/examples/deep_agent)
for a runnable, key-free demonstration that plans, offloads a large artifact,
delegates to a subagent, and completes a report across two turns.
