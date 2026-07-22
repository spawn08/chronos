# Multi-Tenant Memory Isolation

Demonstrates **per-user long-term memory isolation**: two users served by one
logical agent, whose memories never leak across tenants.

```bash
go run ./examples/multitenant_memory/
```

The core demonstration uses SQLite and needs **no API keys**. An optional final
section runs the LLM-powered `memory.Manager` and is guarded behind
`OPENAI_API_KEY`.

## The isolation pattern

Chronos long-term memory (`memory.Store` and `memory.Manager`) is keyed by the
**Store's agent id**. `SetLongTerm` writes a record id of the form
`mem_<agentID>_lt_<key>`, and `ListLongTerm` / `GetUserMemories` read every
long-term record for that agent id. (The `userID` passed to `memory.NewManager`
is not yet used as an isolation key.)

To isolate memory per user today, construct a Store whose id is **namespaced
with the user id**:

```go
const agentID = "concierge"

func userStore(backend storage.Storage, userID string) *memory.Store {
    return memory.NewStore(agentID+"::"+userID, backend)
}
```

Every tenant then reads and writes under its own namespace, so no query can
return another tenant's data.

## What the example verifies

1. Alice and Bob each store distinct long-term memories.
2. `ListLongTerm` for each tenant returns only that tenant's records.
3. A cross-tenant read fails: `alice.Get("plan")` (Bob's key) returns
   "no rows in result set".
4. The same key resolves to different values per tenant
   (`favorite_language`: Alice→Go, Bob→Rust).
5. `memory.Manager.GetUserMemories` produces a context block scoped to one
   tenant (this call reads memory only — it does not invoke the model, so the
   `noopProvider` is never called).

## Optional: autonomous LLM extraction

If `OPENAI_API_KEY` is set, the example also runs
`memory.Manager.ExtractMemories` against a real provider to show the manager
autonomously deciding what to remember from a conversation, storing it under the
tenant's namespace. Without the key, that section prints an instructive message
and is skipped, so the program always completes.

```bash
export OPENAI_API_KEY=sk-...
go run ./examples/multitenant_memory/
```

## Key APIs

- `memory.NewStore(agentID, backend)` and `SetLongTerm` / `ListLongTerm` / `Get`.
- `memory.NewManager(agentID, userID, store, provider)` and `GetUserMemories` /
  `ExtractMemories`.
- `storage/adapters/sqlite` as the backing store.
