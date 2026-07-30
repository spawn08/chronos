---
title: "Virtual Filesystem (Context Offloading)"
sidebar_label: "Virtual Filesystem"
---


On a long task, intermediate work — research notes, drafts, large tool output —
piles up in the context window and burns the token budget. The **virtual
filesystem (VFS)** gives an agent per-session scratch space: it writes large
artifacts *out* of the prompt with `fs_write` (paying only a tiny receipt in
tokens) and pages them back in *on demand* with `fs_read`. This is the second
piece of the Chronos agent harness, after [Planning](/guides/planning).

## Quick start

```go
import (
    "github.com/spawn08/chronos/engine/tool/builtins"
    "github.com/spawn08/chronos/sdk/agent"
)

// Durable, session-scoped scratch space. Requires a storage backend that
// implements storage.SessionFileStore (SQLite and PostgreSQL do).
vfs, err := builtins.NewStorageVFS(store)
if err != nil {
    log.Fatal(err) // backend doesn't support session files
}

a, _ := agent.New("research-agent", "Research Assistant").
    WithModel(provider).
    WithStorage(store).
    AddToolkit(builtins.NewVFSToolkit(vfs)).
    Build()

resp, _ := a.ChatWithSession(ctx, "session-1", "Research topic X and write a report.")
```

`NewVFSToolkit` registers four tools:

| Tool | Purpose | Returns |
|------|---------|---------|
| `fs_write` | Save an artifact under a path | path + byte count (**never the content**) |
| `fs_read` | Page an artifact back into context | path + content |
| `fs_ls` | List saved artifacts by optional prefix | paths + sizes (metadata only) |
| `fs_delete` | Delete an artifact | path + `deleted` |

Because `fs_write` returns only a receipt, offloading a 50 KB artifact costs a
few dozen tokens instead of tens of thousands. The model reads back only what it
needs, when it needs it.

## Stores

The VFS is an interface (`builtins.VFS`) with two implementations:

| Constructor | Durability | Use for |
|-------------|-----------|---------|
| `NewInMemoryVFS()` | process-local, lost on restart | dev, tests, ephemeral chats |
| `NewStorageVFS(store)` | persisted per session in the backend | production, durable runs |

`NewStorageVFS` fails at **construction** (not at first use) if the storage
backend does not implement `storage.SessionFileStore`, so a misconfiguration
surfaces immediately rather than mid-run. Files are stored in a dedicated
`session_files` table — the VFS never touches the run's event ledger or the
session record, so it composes cleanly with [durable execution](/guides/durable-execution)
and the [planning tool](/guides/planning).

## Isolation and lifecycle

Every artifact is keyed by **(tenant, session, path)**. Two tenants — or two
sessions — never see each other's files, the same guarantee as the rest of
[storage](/guides/multi-tenancy). Files live for the life of the session; delete
them explicitly with `fs_delete`, or trim them with the session as part of your
[retention policy](/guides/scaling-best-practices).

A single `fs_write` is capped at `builtins.MaxArtifactBytes` (5 MiB) so a runaway
artifact can't bloat the backing table; larger writes are rejected. `fs_ls`
returns at most `storage.MaxPageLimit` entries per call, and pushes the path
prefix into the query so the listing stays index-backed.

:::note
The VFS **requires an active session** (like planning). `ChatWithSession` and
graph runs set it automatically; calling the plain `Chat` method with the VFS
toolkit registered makes the `fs_*` tools fail with `ErrNoSession` by design, so
an artifact is never written to a shared, sessionless scope.
:::

## Example

A complete, runnable example (no API keys) is in
[`examples/vfs_agent`](https://github.com/spawn08/chronos/tree/main/examples/vfs_agent).
It offloads a 55 KB report to the VFS, showing that only a 51-byte receipt enters
the chat context, then reads it back to produce the final answer.
