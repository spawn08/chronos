# Multi-Round Tool Calling

Demonstrates an agent that performs **multiple sequential tool-calling rounds**,
where the result of one tool feeds the arguments of the next.

```bash
go run ./examples/multi_round_tools/
```

A small deterministic mock `model.Provider` drives the loop, so the example runs
with **no API keys** and **no network access**.

## The scenario

The user asks: *"How many orders does Ada Lovelace have?"* Answering it requires
two tools in sequence:

```
resolve_customer(name="Ada Lovelace")  ──▶  { customer_id: "C-1007", tier: "gold" }
                                                     │  (A feeds B)
                                                     ▼
fetch_orders(customer_id="C-1007")     ──▶  { order_count: 3 }
                                                     │
                                                     ▼
                       "Customer C-1007 has 3 recent orders."
```

`Agent.Chat` runs the tool-calling loop: it executes each requested tool, feeds
the result back to the model, and repeats until the model stops asking for
tools.

## How the mock sequences the rounds

The mock chooses its next action purely from the **most recent message**:

| Most recent message           | Mock's response                                  |
|--------------------------------|--------------------------------------------------|
| the user's question            | call `resolve_customer(name)`                    |
| `resolve_customer` result      | read `customer_id`, call `fetch_orders(id)`      |
| `fetch_orders` result          | emit the final natural-language answer           |

Because the mock reads `resolve_customer`'s output out of the tool-result
message and places the `customer_id` into `fetch_orders`'s arguments, the "A
feeds B" data dependency is real, not scripted with hard-coded ids.

## Key APIs

- `tool.Definition` with a real `Handler` and `PermAllow`.
- `agent.New(...).AddTool(...).WithModel(...)` builder.
- `model.Provider` implemented locally (`Chat`, `StreamChat`, `Name`, `Model`).
- `model.StopReasonToolCall` / `model.StopReasonEnd` to drive the loop.
