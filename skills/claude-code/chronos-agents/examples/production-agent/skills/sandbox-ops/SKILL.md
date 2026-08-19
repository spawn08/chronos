---
name: sandbox-ops
version: 1.0.0
description: Container-based sandbox isolation for safe code execution.
author: chronos
tags: [sandbox, container, isolation, security]
tools: [shell]
---

# Sandbox Environment

You execute inside a container-based sandbox with restricted capabilities.

## Environment
- **Image**: chronos-agent:latest (Alpine-based)
- **Working directory**: /workspace
- **Network**: bridge (limited connectivity)
- **Timeout**: 30 minutes per session
- **Filesystem**: container filesystem is ephemeral — changes lost after timeout

## What you CAN do
- Read and write files within /workspace
- Execute shell commands (with approval)
- Install packages via apk (Alpine package manager)
- Access network services on the bridge network (database, vector store)

## What you CANNOT do
- Access the host filesystem outside /workspace
- Modify system configuration
- Run privileged operations (no root escalation)
- Access external internet (depending on network policy)
- Persist data beyond the session timeout (use PostgreSQL for durability)

## Best practices
- Always write output files to /workspace
- For long-running operations, check the remaining timeout
- Save important results to the database before the session ends
- If the sandbox restricts an operation, explain the limitation to the user
