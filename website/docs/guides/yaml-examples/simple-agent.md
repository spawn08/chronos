---
title: "1. Simple: Single Agent"
sidebar_label: "1. Single agent"
---

Start with the smallest useful Chronos application: one agent, one provider, and one system prompt.

## Create `.chronos/agents.yaml`

```yaml
agents:
  - id: assistant
    name: Personal Assistant
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: none
    system_prompt: |
      You are a friendly personal assistant.
      Be concise and helpful. Use bullet points for lists.
```

## Validate and run

```bash
export OPENAI_API_KEY=sk-your-key-here

# Strict validation catches misspelled or invalid fields.
chronos config validate

# Inspect what loaded.
chronos agent list
chronos agent show assistant

# Send one message.
chronos run "What are three interesting facts about the moon?"

# Start an interactive conversation.
chronos repl
```

Because this file is at `.chronos/agents.yaml`, Chronos discovers it automatically.

## Enable streaming

Set the default in YAML:

```yaml
agents:
  - id: assistant
    name: Personal Assistant
    stream: true
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: none
    system_prompt: You are a concise, helpful assistant.
```

Or override it per command:

```bash
chronos run --stream "Explain goroutines in simple terms"
chronos run --no-stream "Return one complete, validated answer"
```

Inside the REPL, use `/stream on`, `/stream off`, or `/stream` to inspect the current mode.

## Add reusable defaults

When you add more agents, move repeated model and storage settings into `defaults`:

```yaml
defaults:
  model:
    provider: openai
    model: gpt-5.5
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none
  stream: true

agents:
  - id: assistant
    name: Personal Assistant
    system_prompt: You are a concise, helpful assistant.

  - id: summarizer
    name: Summarizer
    system_prompt: Summarize the supplied text in five bullets or fewer.
```

```bash
chronos agent list
chronos run --agent summarizer "Summarize: ..."
```

## Try a local model

Ollama requires no API key:

```yaml
agents:
  - id: local-assistant
    name: Local Assistant
    model:
      provider: ollama
      model: llama3.2
      base_url: http://localhost:11434
    storage:
      backend: none
    system_prompt: You are a helpful local assistant.
```

```bash
ollama pull llama3.2
chronos config validate
chronos run "Say hello from a local model"
```

## What to learn next

- Need one of several specialists? Build a [customer-support router](./intermediate-workflows#customer-support-router).
- Need every stage to run? Build a [sequential content pipeline](./intermediate-workflows#content-creation-pipeline).
- Need tool safety and traces? Move to [production application patterns](./production-applications).
