---
title: "3. Advanced: Multi-Agent Teams"
sidebar_label: "3. Advanced teams"
---

Advanced teams plan, delegate, run concurrently, or hand work between peers. Keep each application in its own YAML file so policies and prompts stay understandable.

## Software-development coordinator

A technical lead decomposes a feature, delegates to specialists, and reviews results.

### Create `coding-team.yaml`

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
  - id: tech-lead
    name: Technical Lead
    description: Plans architecture and coordinates the development team
    system_prompt: |
      You are a senior technical lead. For each feature request:
      1. Break it into actionable sub-tasks
      2. Assign the right specialist
      3. Specify dependencies and acceptance criteria
      4. Review results before completing the task
    capabilities: [architecture, planning]

  - id: backend-dev
    name: Backend Developer
    description: Implements server-side Go code and APIs
    system_prompt: |
      You are an expert backend developer. Write clean, idiomatic Go.
      Include input validation, wrapped errors, and table-driven tests.
    capabilities: [backend, golang]

  - id: frontend-dev
    name: Frontend Developer
    description: Implements accessible React interfaces
    system_prompt: |
      You are a senior frontend developer. Write TypeScript and React.
      Prioritize accessibility, loading/error states, and responsive design.
    capabilities: [frontend, react]

  - id: code-reviewer
    name: Code Reviewer
    description: Reviews correctness, security, performance, and maintainability
    system_prompt: |
      Review the proposed implementation for correctness, security,
      performance, test coverage, and maintainability. Return prioritized findings.
    capabilities: [code-review, security]

teams:
  - id: dev-team
    name: Development Team
    strategy: coordinator
    coordinator: tech-lead
    agents:
      - backend-dev
      - frontend-dev
      - code-reviewer
    max_iterations: 2
```

```bash
chronos -c coding-team.yaml config validate
chronos -c coding-team.yaml team run --stream dev-team \
  "Build email/password registration with an API and accessible form"
```

`max_iterations: 2` allows the lead to review one round and re-plan once.

## Multi-provider parallel comparison

Run the same prompt through several providers concurrently. `best_effort` keeps successful results when one provider is unavailable.

### Create `multi-provider.yaml`

```yaml
agents:
  - id: openai-agent
    name: GPT Agent
    model:
      provider: openai
      model: gpt-5.5
      api_key: ${OPENAI_API_KEY}
    storage: {backend: none}
    system_prompt: Give a precise answer and state important assumptions.

  - id: claude-agent
    name: Claude Agent
    model:
      provider: anthropic
      model: claude-opus-4-8
      api_key: ${ANTHROPIC_API_KEY}
    storage: {backend: none}
    system_prompt: Give a precise answer and state important assumptions.

  - id: gemini-agent
    name: Gemini Agent
    model:
      provider: gemini
      model: gemini-2.0-flash
      api_key: ${GEMINI_API_KEY}
    storage: {backend: none}
    system_prompt: Give a precise answer and state important assumptions.

  - id: azure-agent
    name: Azure OpenAI Agent
    model:
      provider: azure
      deployment: my-gpt4o-deployment
      endpoint: https://my-resource.openai.azure.com
      api_version: "2024-10-21"
      api_key: ${AZURE_OPENAI_API_KEY}
    storage: {backend: none}
    system_prompt: Give a precise answer and state important assumptions.

  - id: grok-agent
    name: Grok Agent
    model:
      provider: compatible
      model: grok-4
      base_url: https://api.x.ai/v1
      api_key: ${XAI_API_KEY}
    storage: {backend: none}
    system_prompt: Give a precise answer and state important assumptions.

teams:
  - id: compare
    name: Provider Comparison
    strategy: parallel
    agents:
      - openai-agent
      - claude-agent
      - gemini-agent
      - azure-agent
      - grok-agent
    max_concurrency: 5
    error_strategy: best_effort
```

```bash
chronos -c multi-provider.yaml config validate
chronos -c multi-provider.yaml team run --stream compare \
  "Explain quantum entanglement in two sentences"
```

See [provider recipes](./providers) for every supported model backend.

## Incident-response swarm

A swarm lets peers transfer ownership dynamically instead of following a fixed plan.

### Create `incident-swarm.yaml`

```yaml
defaults:
  model:
    provider: openai
    model: gpt-5.5
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none

agents:
  - id: triage
    name: Incident Triage
    description: Classifies severity, gathers evidence, and selects the next owner
    system_prompt: |
      Triage the incident. Identify severity, affected services, evidence gaps,
      and the specialist who should take ownership next.
    capabilities: [triage, incident-management]

  - id: application
    name: Application Engineer
    description: Diagnoses application errors, regressions, and bad deployments
    system_prompt: Diagnose application-level causes and propose reversible mitigations.
    capabilities: [application, debugging]

  - id: infrastructure
    name: Infrastructure Engineer
    description: Diagnoses compute, network, Kubernetes, and database failures
    system_prompt: Diagnose infrastructure causes and propose safe mitigations.
    capabilities: [infrastructure, kubernetes, networking]

teams:
  - id: incident-swarm
    name: Incident Response Swarm
    strategy: swarm
    agents: [triage, application, infrastructure]
    initial_agent: triage
    max_handoffs: 6
```

```bash
chronos -c incident-swarm.yaml config validate
chronos -c incident-swarm.yaml team run incident-swarm \
  "Checkout latency doubled after the latest deployment"
```

Swarm routing depends on model tool-call output, so use completed-output mode rather than token streaming.

## Engineering hierarchy

A hierarchy models a root supervisor with specialist workers.

### Create `engineering-hierarchy.yaml`

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
  - id: engineering-director
    name: Engineering Director
    description: Owns architecture, priorities, and final integration decisions
    system_prompt: Delegate work to specialists, reconcile results, and return one plan.

  - id: platform-team
    name: Platform Team
    description: Owns runtime, deployment, reliability, and observability
    system_prompt: Produce the platform and operations portion of the solution.

  - id: product-team
    name: Product Team
    description: Owns APIs, user workflows, and application behavior
    system_prompt: Produce the product and application portion of the solution.

  - id: security-team
    name: Security Team
    description: Owns threat modeling, controls, and compliance requirements
    system_prompt: Review the plan and specify required security controls.

teams:
  - id: engineering-org
    name: Engineering Organization
    strategy: hierarchy
    coordinator: engineering-director
    agents:
      - engineering-director
      - platform-team
      - product-team
      - security-team
    error_strategy: collect
```

```bash
chronos -c engineering-hierarchy.yaml config validate
chronos -c engineering-hierarchy.yaml team run --stream engineering-org \
  "Design a multi-tenant document processing platform"
```

## Strategy selection

| Strategy | Ownership model | Typical application |
|----------|-----------------|---------------------|
| `parallel` | Independent peers | Model comparison, review panels |
| `coordinator` | One planner delegates and reviews | Engineering projects, complex analysis |
| `swarm` | Peers hand off dynamically | Incident response, open-ended investigation |
| `hierarchy` | Root supervisor delegates down | Organization-style planning |

Next, add tool policy, reasoning, tracing, and sandboxing with [production application patterns](./production-applications).
