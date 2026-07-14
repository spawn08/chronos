# Chronos Development

## Status

The original seven-tier build-out plan (agent wiring → embeddings → CLI → ChronosOS →
storage adapters → sandbox → production hardening) is **complete**. All 101 tracked
roadmap items were delivered; see [ROADMAP.md](ROADMAP.md) for the capability history.

## Current focus

Forward-looking work is tracked in **[PLAN.md](PLAN.md)**: production-hardening and
high-scale readiness (durable execution, correctness bug fixes, model-serving
robustness, storage/data plane, control-plane security, observability, sandbox
hardening, and delivery/supply-chain). Start there before picking up new work.

## Workflow

- Follow the conventions and "Do NOT" rules in [CLAUDE.md](CLAUDE.md).
- Run `./scripts/pre-push-check.sh` before pushing (format + vet + lint + build + test).
- Scaffolding slash commands live in `.claude/commands/` (see CLAUDE.md).
