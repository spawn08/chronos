---
title: "Observability & CLI"
---


# Observability & CLI

Production plumbing (metrics, cost, caching, retries) and operating Chronos from the command line. All run with **no API key**.

---

## hooks_observability

All 6 hooks in action: metrics, cost tracking, rate limiting, caching, retry, and structured logging.

```bash
go run ./examples/hooks_observability/
```

**Demonstrates:**
- `MetricsHook` — latency tracking, call counting, per-call metrics
- `CostTracker` — per-model pricing, budget limits, token accounting
- `CacheHook` — LLM response caching with TTL and max entries
- `RateLimitHook` — token-bucket rate limiting
- `RetryHook` — exponential backoff with retry callbacks
- `LoggingHook` — structured event logging
- Hook chain composition

See the [Cost Tracking](/guides/cost-tracking/) and [Hooks](/guides/hooks/) guides for details.

---

## cli_agent

Build, inspect, and run an agent from YAML via the Chronos CLI.

```bash
go run ./examples/cli_agent/
```

**Demonstrates:**
- Loading an agent definition from YAML
- Inspecting the resolved agent configuration
- Running it headlessly from the CLI

Source: [examples/cli_agent](https://github.com/spawn08/chronos/tree/main/examples/cli_agent)

---

## cli_ops

Operate Chronos from the CLI: serve the control plane, monitor runs, inspect the database, manage sessions, pipe input, and deploy teams.

```bash
go run ./examples/cli_ops/
```

**Demonstrates:**
- `serve` — start the ChronosOS control plane
- `monitor` — watch live runs and events
- `db` / `sessions` — inspect persisted state
- `pipe` — headless batch input
- `deploy` — launch a team from a deployment config

Source: [examples/cli_ops](https://github.com/spawn08/chronos/tree/main/examples/cli_ops)
