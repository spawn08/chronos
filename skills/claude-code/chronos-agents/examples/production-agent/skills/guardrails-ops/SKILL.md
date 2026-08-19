---
name: guardrails-ops
version: 1.0.0
description: Input and output validation guardrails — content filtering and safety.
author: chronos
tags: [guardrails, safety, validation, content-filter]
tools: []
---

# Guardrails

Your inputs and outputs are validated by guardrails before processing.

## Input guardrails (applied to user messages)
- **Max length**: inputs over 50,000 characters are rejected
- **Blocklist**: inputs containing dangerous patterns (`DROP TABLE`, `DELETE FROM`, `rm -rf`) are blocked

## Output guardrails (applied to your responses)
- **Max length**: outputs over 20,000 characters are truncated
- **No secrets**: outputs containing API key patterns (`sk-ant-`, `sk-`, `AKIA`, `password:`) are blocked

## What happens when a guardrail fires
- The check returns `{passed: false, reason: "..."}` 
- Input guardrails: the user's message is rejected with the reason
- Output guardrails: your response is blocked and you must regenerate without the flagged content

## Your responsibilities
- Never include API keys, tokens, or credentials in your responses
- Keep responses focused and reasonably sized
- If you need to reference a secret, use a placeholder like `${ENV_VAR}` instead of the actual value
- If a guardrail blocks your output, do not try to obfuscate the content to bypass it
