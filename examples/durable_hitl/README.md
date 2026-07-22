# Durable Human-in-the-Loop (HITL)

Demonstrates a **durable** StateGraph that pauses for human approval, persists a
checkpoint to SQLite, and is later resumed — by a fresh `Runner` simulating a
process restart — all the way to completion.

```bash
go run ./examples/durable_hitl/
```

No API keys and no network access are required: every graph node is a pure Go
function, so the run is fully deterministic.

## What it shows

- `graph.StateGraph` with an **interrupt node** (`AddInterruptNode`).
- `graph.Runner.Run` starting a run that pauses at the interrupt gate.
- Durable checkpointing to the `storage/adapters/sqlite` backend (a temp file,
  so we can close it and reopen a brand-new store before resuming).
- `graph.Runner.Resume` restoring the persisted state and finishing the flow.

## The workflow

```
prepare ──▶ approval (gate) ──▶ disburse ──▶ END
```

1. `prepare` builds the approval request and marks status `pending_approval`.
2. `approval` is the human gate.
3. `disburse` performs the action once approval is granted.

## Checkpoint / resume flow (and an important detail)

In Chronos an **interrupt node checkpoints and pauses _before_ its function
runs** — it is the point at which a human must approve. The runner stores a
`Checkpoint` (run id, node id, full state, sequence number) in SQLite and
returns with `RunStatus == RunStatusPaused`.

When you resume, the runner reloads the latest checkpoint and continues. There
is one subtlety worth understanding:

> If you resume against a graph where the gate is **still** an interrupt node,
> the runner re-pauses at the gate — because the approval has not been granted
> yet. (This is the current behavior of `engine/graph`.)

To advance **past** the gate, this example resumes against an *approved variant*
of the graph in which the gate is an ordinary node that records the human's
decision (`approved = true`, the approver, etc.) and lets execution flow onward.
This mirrors how real HITL systems work:

- **Paused checkpoint** = the request for approval, durably persisted.
- **Resume with the approval granted** = continue the work from exactly where it
  stopped, with no earlier node re-executed.

The two graphs (`pendingGraph` / `approvedGraph`) share identical node IDs, so
the checkpoint taken on the pending graph resumes cleanly on the approved one.

## Expected output (abridged)

```
[run] paused — a durable checkpoint is now persisted at .../hitl.db
[resume] loaded checkpoint node="approval" seq=2 state=map[... status:pending_approval]
  [node:approval] approval granted — recording decision
  [node:disburse] disbursing $4200 to Ada (approved by manager@corp.example)
[resume] status=completed
```
