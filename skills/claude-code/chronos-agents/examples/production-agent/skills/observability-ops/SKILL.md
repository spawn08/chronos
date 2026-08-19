---
name: observability-ops
version: 1.0.0
description: Tracing, audit logging, and monitoring for production observability.
author: chronos
tags: [tracing, audit, monitoring, observability]
tools: []
---

# Observability

All your actions are traced and logged for operational visibility.

## What is tracked
- **Spans**: each tool call, model call, and graph node execution creates a span with timing data
- **Audit logs**: immutable record of every significant action (tool calls, approvals, errors)
- **Events**: streaming event log for real-time monitoring
- **Session data**: full conversation history with metadata

## Your responsibilities
- For complex operations, explain your reasoning before acting — this helps operators understand the trace
- When something fails, include the error context in your response — it will be logged
- Do not attempt to modify, delete, or suppress audit entries

## Monitoring endpoints
The control plane exposes:
- `GET /health` — system health check
- `GET /api/sessions` — active sessions
- `GET /api/sessions/:id/traces` — trace spans for a session
- `GET /api/sessions/:id/audit` — audit log entries

Operators can use these to investigate your behavior and debug issues.
