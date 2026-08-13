---
title: "2. Intermediate: Routers & Pipelines"
sidebar_label: "2. Routers & pipelines"
---

Intermediate applications introduce specialist agents and explicit collaboration. This page covers the two most common patterns: selecting one specialist and running a fixed sequence.

## Customer-support router

Three specialist agents handle billing, technical, and sales questions. The router dispatches each request to exactly one agent.

### Create `customer-support.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-5.5
  storage:
    backend: none
  stream: true

agents:
  - id: billing-support
    name: Billing Support Agent
    description: Handles invoices, payments, refunds, and subscription changes
    system_prompt: |
      You are a billing support specialist at a SaaS company.

      Your responsibilities:
      - Answer questions about invoices and billing cycles
      - Process refund requests after collecting the order ID and reason
      - Explain pricing tiers and subscription changes

      Always be polite. Ask for the customer's account ID first.
    capabilities: [billing, payments, refunds]

  - id: technical-support
    name: Technical Support Agent
    description: Diagnoses bugs, errors, and technical issues
    system_prompt: |
      You are a senior technical support engineer.

      Your approach:
      1. Ask clarifying questions about the issue
      2. Check common causes
      3. Provide step-by-step troubleshooting
      4. If unresolved, suggest filing a bug report

      Ask for error messages, reproduction steps, and OS version.
    capabilities: [debugging, troubleshooting]

  - id: sales-support
    name: Sales Agent
    description: Handles pricing questions, demos, and plan upgrades
    system_prompt: |
      You are a friendly sales representative.

      Pricing:
      - Starter: $29/month (5 users, 10GB)
      - Pro: $99/month (25 users, 100GB)
      - Enterprise: Custom pricing (unlimited)

      Understand the customer's needs before recommending a plan.
    capabilities: [sales, pricing]

teams:
  - id: support
    name: Customer Support Router
    strategy: router
    router: model
    router_model:
      provider: openai
      model: gpt-4o-mini
      api_key: ${OPENAI_API_KEY}
    agents:
      - billing-support
      - technical-support
      - sales-support
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here
chronos -c customer-support.yaml config validate
chronos -c customer-support.yaml team list

chronos -c customer-support.yaml team run --stream support \
  "I was charged twice on my last invoice"
chronos -c customer-support.yaml team run support \
  "The app crashes when I export a PDF"
chronos -c customer-support.yaml team run support \
  "What's the difference between Pro and Enterprise?"
```

### How routing works

- `router: model` lets an LLM read agent names, descriptions, and capabilities before selecting one specialist.
- `router_model` uses a cheaper model for dispatch while workers keep their configured model.
- `router: capability` is a zero-LLM heuristic for callers that set explicit capability keys in graph state. It does not interpret free-form message intent.

:::note Router versus pipeline
A router chooses **one** agent. If every stage must execute, use `strategy: sequential`.
:::

## Content-creation pipeline

A researcher gathers facts, a writer drafts an article, and an editor polishes it. Each agent receives the previous stage's output.

### Create `content-pipeline.yaml`

```yaml
defaults:
  model:
    provider: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-5.5
  storage:
    backend: none
  stream: true

agents:
  - id: researcher
    name: Research Analyst
    description: Researches topics and provides factual analysis
    system_prompt: |
      You are a research analyst.
      Given a topic, provide five key facts with specific numbers or data.
      Format them as a numbered list. Do not add unsupported opinions.
    capabilities: [research]

  - id: writer
    name: Content Writer
    description: Writes articles from research notes
    system_prompt: |
      You are a professional writer.
      Given research notes, write a 300-500 word article with:
      - An engaging opening
      - Clear headers
      - A forward-looking conclusion
      Do not invent facts. Use only the supplied research.
    capabilities: [writing]

  - id: editor
    name: Senior Editor
    description: Reviews and improves content
    system_prompt: |
      You are a senior editor. Improve the supplied article:
      - Fix grammar and spelling
      - Improve flow and readability
      - Tighten wordy sections
      Return only the final polished version.
    capabilities: [editing]

teams:
  - id: pipeline
    name: Content Pipeline
    strategy: sequential
    agents:
      - researcher
      - writer
      - editor
```

### Run it

```bash
export OPENAI_API_KEY=sk-your-key-here
chronos -c content-pipeline.yaml config validate
chronos -c content-pipeline.yaml team show pipeline
chronos -c content-pipeline.yaml team run --stream pipeline \
  "Write a short article about the rise of electric vehicles"
```

### Data flow

```text
Topic → Researcher → Writer → Editor → Final article
          facts        draft     polish
```

Use sequential teams for document processing, enrichment pipelines, compliance review, and any workflow with a stable order.

## Run the bundled files

These complete configs are included in the repository:

```bash
chronos -c examples/yaml-configs/customer-support.yaml config validate
chronos -c examples/yaml-configs/content-pipeline.yaml config validate

chronos -c examples/yaml-configs/customer-support.yaml \
  team run support "I need a refund for order #12345"

chronos -c examples/yaml-configs/content-pipeline.yaml \
  team run pipeline "Write about renewable energy trends"
```

## Next

When work must be planned, delegated, retried, or handled dynamically, continue to [advanced multi-agent teams](./advanced-multi-agent).
