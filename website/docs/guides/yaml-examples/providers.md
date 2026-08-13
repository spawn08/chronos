---
title: "Provider YAML Recipes"
sidebar_label: "Provider recipes"
---

Drop one of these `model:` blocks into an agent or `defaults.model`. All string values support `${ENV_VAR}` expansion.

:::tip Runnable provider files
Standalone configurations live under [`examples/yaml-configs/providers/`](https://github.com/spawn08/chronos/tree/main/examples/yaml-configs/providers).

```bash
export ANTHROPIC_API_KEY=sk-ant-...
chronos -c examples/yaml-configs/providers/anthropic.yaml run "Hello"
```
:::

## Provider matrix

| Provider | `provider` value | Authentication |
|----------|------------------|----------------|
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Anthropic | `anthropic` | `ANTHROPIC_API_KEY` |
| Gemini AI Studio | `gemini` | `GEMINI_API_KEY` |
| Vertex AI | `compatible` | `GOOGLE_ACCESS_TOKEN` |
| Azure OpenAI | `azure` | `AZURE_OPENAI_API_KEY` |
| Grok / xAI | `compatible` | `XAI_API_KEY` |
| Groq | `groq` | `GROQ_API_KEY` |
| Mistral | `mistral` | `MISTRAL_API_KEY` |
| DeepSeek | `deepseek` | `DEEPSEEK_API_KEY` |
| Ollama | `ollama` | None |
| Other OpenAI-compatible APIs | `compatible` | Provider-specific key |

## OpenAI

```yaml
model:
  provider: openai
  model: gpt-5.5
  api_key: ${OPENAI_API_KEY}
  timeout_sec: 120
```

Native reasoning:

```yaml
reasoning:
  native: true
  effort: high
  summary: false
```

## Anthropic

```yaml
model:
  provider: anthropic
  model: claude-opus-4-8
  api_key: ${ANTHROPIC_API_KEY}
  timeout_sec: 120
```

Extended thinking for a tool-free agent:

```yaml
reasoning:
  native: true
  budget_tokens: 4096
  summary: true
```

Anthropic native reasoning with tools currently fails closed because signed thinking blocks cannot yet be preserved across tool rounds. Use prompt strategy reasoning when tools are present.

## Google Gemini (AI Studio)

```yaml
model:
  provider: gemini
  model: gemini-2.0-flash
  api_key: ${GEMINI_API_KEY}
```

Thinking for a tool-free agent:

```yaml
reasoning:
  native: true
  budget_tokens: 4096
  summary: true
```

Gemini native reasoning with tools has the same signed-thought limitation as Anthropic.

## Google Vertex AI

Vertex exposes an OpenAI-compatible endpoint. Use a short-lived Google access token:

```yaml
model:
  provider: compatible
  model: google/gemini-2.0-flash
  base_url: https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT/locations/us-central1/endpoints/openapi
  api_key: ${GOOGLE_ACCESS_TOKEN}
```

```bash
export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
```

## Azure OpenAI

Azure requires the resource endpoint, deployment name, and API version:

```yaml
model:
  provider: azure
  deployment: my-gpt4o-deployment
  endpoint: https://my-resource.openai.azure.com
  api_version: "2024-10-21"
  api_key: ${AZURE_OPENAI_API_KEY}
```

## Grok (xAI)

```yaml
model:
  provider: compatible
  model: grok-4
  base_url: https://api.x.ai/v1
  api_key: ${XAI_API_KEY}
```

## Groq

```yaml
model:
  provider: groq
  model: llama-3.3-70b-versatile
  api_key: ${GROQ_API_KEY}
```

## Mistral

```yaml
model:
  provider: mistral
  model: mistral-large-latest
  api_key: ${MISTRAL_API_KEY}
```

## DeepSeek

```yaml
model:
  provider: deepseek
  model: deepseek-chat
  api_key: ${DEEPSEEK_API_KEY}
```

## Ollama

```yaml
model:
  provider: ollama
  model: llama3.2
  base_url: http://localhost:11434
```

```bash
ollama pull llama3.2
chronos -c examples/yaml-configs/providers/ollama.yaml run "Hello locally"
```

## Other OpenAI-compatible endpoints

Use `compatible` for Together, OpenRouter, Fireworks, Perplexity, vLLM, LiteLLM, and other Chat Completions-compatible APIs:

```yaml
model:
  provider: compatible
  model: meta-llama/Llama-3.3-70B-Instruct-Turbo
  base_url: https://api.together.xyz/v1
  api_key: ${TOGETHER_API_KEY}
```

## Mix providers in one team

Each agent owns its own `model` block, so a parallel team can compare providers:

```yaml
teams:
  - id: compare
    name: Provider Comparison
    strategy: parallel
    agents: [openai-agent, claude-agent, gemini-agent]
    max_concurrency: 3
    error_strategy: best_effort
```

See the complete [multi-provider application](./advanced-multi-agent#multi-provider-parallel-comparison).
