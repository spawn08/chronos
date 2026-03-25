---
title: "Building Real-World Agents"
permalink: /guides/real-world-agents/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

This guide walks you through building production-quality agents with **real LLM providers** — no mocks, no stubs. Every example in this guide compiles, runs, and makes actual LLM API calls. You'll see both the **Go code** and the **YAML equivalent** for each pattern.

## Environment Setup

Before running any example, you need exactly one thing: an LLM API key.

### Supported Providers

| Provider | Env Variable | Default Model | Sign Up |
|----------|-------------|---------------|---------|
| **OpenAI** | `OPENAI_API_KEY` | `gpt-4o` | [platform.openai.com](https://platform.openai.com) |
| **Anthropic** | `ANTHROPIC_API_KEY` | `claude-sonnet-4-20250514` | [console.anthropic.com](https://console.anthropic.com) |
| **Google Gemini** | `GEMINI_API_KEY` | `gemini-2.0-flash` | [ai.google.dev](https://ai.google.dev) |
| **Mistral** | `MISTRAL_API_KEY` | `mistral-large-latest` | [console.mistral.ai](https://console.mistral.ai) |
| **Groq** | `GROQ_API_KEY` | *(your choice)* | [console.groq.com](https://console.groq.com) |
| **DeepSeek** | `DEEPSEEK_API_KEY` | *(your choice)* | [platform.deepseek.com](https://platform.deepseek.com) |
| **Azure OpenAI** | `AZURE_OPENAI_API_KEY` + `AZURE_OPENAI_ENDPOINT` + `AZURE_OPENAI_DEPLOYMENT` | *(deployment)* | [portal.azure.com](https://portal.azure.com) |
| **Ollama** | *(none — fully local)* | `llama3.2` | [ollama.ai](https://ollama.ai) |

### Quick Start

```bash
# Clone the repo
git clone https://github.com/spawn08/chronos.git
cd chronos

# Set your API key (pick ONE)
export OPENAI_API_KEY=sk-your-key-here
# OR
export ANTHROPIC_API_KEY=sk-ant-your-key-here
# OR
export GEMINI_API_KEY=AIza-your-key-here
# OR (no key needed — fully local)
ollama serve && ollama pull llama3.2

# Verify the build
go build ./...
```

### Choosing a Provider in Code

Every example in this guide uses the same pattern: detect which API key is set and connect to that provider. The agent code never changes — only the environment variable.

```go
func resolveProvider() model.Provider {
    if key := os.Getenv("OPENAI_API_KEY"); key != "" {
        return model.NewOpenAI(key)
    }
    if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
        return model.NewAnthropic(key)
    }
    if key := os.Getenv("GEMINI_API_KEY"); key != "" {
        return model.NewGemini(key)
    }
    // No API key — use Ollama locally
    return model.NewOllama("http://localhost:11434", "llama3.2")
}
```

### Choosing a Provider in YAML

```yaml
# OpenAI
model:
  provider: openai
  model: gpt-4o
  api_key: ${OPENAI_API_KEY}

# Anthropic
model:
  provider: anthropic
  model: claude-sonnet-4-20250514
  api_key: ${ANTHROPIC_API_KEY}

# Gemini
model:
  provider: gemini
  model: gemini-2.0-flash
  api_key: ${GEMINI_API_KEY}

# Ollama (no API key, fully local)
model:
  provider: ollama
  model: llama3.2
  base_url: http://localhost:11434

# Azure OpenAI
model:
  provider: azure
  api_key: ${AZURE_OPENAI_API_KEY}
  endpoint: ${AZURE_OPENAI_ENDPOINT}
  deployment: ${AZURE_OPENAI_DEPLOYMENT}
  api_version: "2024-10-21"

# Groq (OpenAI-compatible)
model:
  provider: groq
  model: llama-3.1-70b-versatile
  api_key: ${GROQ_API_KEY}

# Any OpenAI-compatible endpoint
model:
  provider: compatible
  model: your-model-id
  api_key: ${YOUR_API_KEY}
  base_url: https://your-endpoint.com/v1
```

---

## Pattern 1: Chat Agent (Simplest Real Agent)

The simplest useful agent: connect an LLM, set a system prompt, and chat.

### Go Code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    a, err := agent.New("assistant", "Assistant").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant. Be concise.").
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What are the three laws of thermodynamics?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
    fmt.Printf("Tokens: %d prompt + %d completion\n",
        resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
}
```

### YAML Equivalent

```yaml
# .chronos/agents.yaml
agents:
  - id: assistant
    name: Assistant
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: none
    system_prompt: You are a helpful assistant. Be concise.
```

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go run "What are the three laws of thermodynamics?"
```

---

## Pattern 2: Agent with Tools

Give the LLM the ability to call functions. Chronos handles the tool-call loop automatically: the model requests a tool call, Chronos executes the handler, and sends the result back.

### Go Code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/engine/tool"
    "github.com/spawn08/chronos/sdk/agent"
)

func main() {
    ctx := context.Background()

    a, err := agent.New("tool-agent", "Tool Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithSystemPrompt("You are a helpful assistant with access to tools.").
        AddTool(&tool.Definition{
            Name:        "get_time",
            Description: "Get the current date and time in UTC",
            Permission:  tool.PermAllow,
            Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
            Handler: func(_ context.Context, _ map[string]any) (any, error) {
                return time.Now().UTC().Format(time.RFC3339), nil
            },
        }).
        AddTool(&tool.Definition{
            Name:        "calculate",
            Description: "Evaluate a mathematical expression. Supports +, -, *, /.",
            Permission:  tool.PermAllow,
            Parameters: map[string]any{
                "type": "object",
                "properties": map[string]any{
                    "expression": map[string]any{
                        "type":        "string",
                        "description": "Math expression like '2 + 2' or '100 / 3'",
                    },
                },
                "required": []string{"expression"},
            },
            Handler: func(_ context.Context, args map[string]any) (any, error) {
                expr, _ := args["expression"].(string)
                return fmt.Sprintf("Result of '%s': (computed by your math engine)", expr), nil
            },
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    resp, err := a.Chat(ctx, "What time is it right now?")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(resp.Content)
}
```

### YAML Equivalent

YAML agents can reference built-in tools by name. For custom tools with handlers, you need Go code.

```yaml
agents:
  - id: tool-agent
    name: Tool Agent
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    system_prompt: You are a helpful assistant with access to tools.
    tools:
      - name: shell
        description: Execute shell commands
      - name: file_read
        description: Read file contents
      - name: file_write
        description: Write to a file
```

Built-in tool names: `shell`, `shell_auto`, `file_read`, `file_write`, `file_list`, `file_glob`, `file_grep`.

---

## Pattern 3: StateGraph with LLM Nodes

This is the most powerful pattern in Chronos. A **StateGraph** defines a multi-step workflow where each node can call the LLM, use tools, or run arbitrary logic. Conditional edges route execution based on LLM output. Every node is checkpointed to durable storage.

### Architecture

```
User Input
    │
    ▼
┌──────────┐      ┌───────────────┐
│ classify  │─────→│ conditional   │
│ (LLM call)│      │   edge        │
└──────────┘      └───┬───────┬───┘
                      │       │
              technical│       │general
                      ▼       ▼
              ┌──────────┐  ┌──────────┐
              │ technical │  │ general  │
              │ (LLM+tools)│  │ (LLM)   │
              └──────────┘  └──────────┘
                      │       │
                      ▼       ▼
                    ┌──────────┐
                    │   END    │
                    └──────────┘
```

### Go Code (Full Runnable Example)

This is the `examples/graph_with_llm/` example. Each graph node makes a real LLM call.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/engine/tool"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    // 1. Connect to your LLM provider
    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    // 2. Set up storage for checkpointing
    store, _ := sqlite.New(":memory:")
    defer store.Close()
    store.Migrate(ctx)

    // 3. Register tools the LLM can call
    registry := tool.NewRegistry()
    registry.Register(&tool.Definition{
        Name:        "search_docs",
        Description: "Search Go documentation",
        Permission:  tool.PermAllow,
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "query": map[string]any{"type": "string"},
            },
            "required": []string{"query"},
        },
        Handler: func(_ context.Context, args map[string]any) (any, error) {
            query, _ := args["query"].(string)
            return map[string]any{"result": "Documentation for: " + query}, nil
        },
    })

    // 4. Build the StateGraph
    g := graph.New("classifier-workflow")

    // Node: classify the question using LLM
    g.AddNode("classify", func(ctx context.Context, s graph.State) (graph.State, error) {
        question, _ := s["question"].(string)

        resp, err := provider.Chat(ctx, &model.ChatRequest{
            Messages: []model.Message{
                {Role: model.RoleSystem, Content: `Classify as "technical" or "general". Reply with one word only.`},
                {Role: model.RoleUser, Content: question},
            },
        })
        if err != nil {
            return s, err
        }

        category := strings.TrimSpace(strings.ToLower(resp.Content))
        if category != "technical" {
            category = "general"
        }
        s["category"] = category
        return s, nil
    })

    // Node: answer technical questions with tool access
    g.AddNode("technical", func(ctx context.Context, s graph.State) (graph.State, error) {
        question, _ := s["question"].(string)

        req := &model.ChatRequest{
            Messages: []model.Message{
                {Role: model.RoleSystem, Content: "You are a Go expert. Use search_docs when helpful."},
                {Role: model.RoleUser, Content: question},
            },
        }
        // Add tool definitions to the request
        for _, t := range registry.List() {
            req.Tools = append(req.Tools, model.ToolDefinition{
                Type: "function",
                Function: model.FunctionDef{
                    Name:        t.Name,
                    Description: t.Description,
                    Parameters:  t.Parameters,
                },
            })
        }

        resp, err := provider.Chat(ctx, req)
        if err != nil {
            return s, err
        }

        // If model wants to call tools, execute them and call the model again
        if resp.StopReason == model.StopReasonToolCall {
            messages := req.Messages
            messages = append(messages, model.Message{
                Role: model.RoleAssistant, ToolCalls: resp.ToolCalls,
            })
            for _, tc := range resp.ToolCalls {
                var args map[string]any
                json.Unmarshal([]byte(tc.Arguments), &args)
                result, _ := registry.Execute(ctx, tc.Name, args)
                resultJSON, _ := json.Marshal(result)
                messages = append(messages, model.Message{
                    Role: model.RoleTool, Content: string(resultJSON),
                    ToolCallID: tc.ID, Name: tc.Name,
                })
            }
            resp, err = provider.Chat(ctx, &model.ChatRequest{Messages: messages})
            if err != nil {
                return s, err
            }
        }

        s["response"] = resp.Content
        return s, nil
    })

    // Node: answer general questions
    g.AddNode("general", func(ctx context.Context, s graph.State) (graph.State, error) {
        question, _ := s["question"].(string)
        resp, err := provider.Chat(ctx, &model.ChatRequest{
            Messages: []model.Message{
                {Role: model.RoleSystem, Content: "You are a helpful assistant. Be concise."},
                {Role: model.RoleUser, Content: question},
            },
        })
        if err != nil {
            return s, err
        }
        s["response"] = resp.Content
        return s, nil
    })

    // Wire the graph
    g.SetEntryPoint("classify")
    g.AddConditionalEdge("classify", func(s graph.State) string {
        if s["category"] == "technical" {
            return "technical"
        }
        return "general"
    })
    g.SetFinishPoint("technical")
    g.SetFinishPoint("general")

    // 5. Build agent and run
    a, _ := agent.New("graph-agent", "Graph Agent").
        WithStorage(store).
        WithGraph(g).
        Build()

    result, err := a.Run(ctx, map[string]any{
        "question": "What are goroutines in Go?",
    })
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Category: %s\n", result.State["category"])
    fmt.Printf("Response: %s\n", result.State["response"])
}
```

### Run It

```bash
# With OpenAI
export OPENAI_API_KEY=sk-your-key
go run ./examples/graph_with_llm/

# With Anthropic
export ANTHROPIC_API_KEY=sk-ant-your-key
go run ./examples/graph_with_llm/

# With Ollama (no API key, fully local)
ollama serve && ollama pull llama3.2
go run ./examples/graph_with_llm/
```

### YAML Equivalent

The router strategy in YAML provides the same classification behavior as the Go graph with conditional edges:

```yaml
# examples/yaml-configs/graph-agent.yaml
defaults:
  model:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none

agents:
  - id: technical-expert
    name: Technical Expert
    description: Answers programming and Go questions with code examples
    system_prompt: |
      You are a Go programming expert. Provide clear, accurate answers
      with code examples when helpful.
    capabilities:
      - golang
      - programming
      - technical

  - id: general-assistant
    name: General Assistant
    description: Answers general knowledge questions concisely
    system_prompt: |
      You are a helpful assistant. Give concise, accurate answers.
    capabilities:
      - general-knowledge
      - conversation

teams:
  - id: classifier-team
    name: Smart Question Router
    strategy: router
    agents:
      - technical-expert
      - general-assistant
```

```bash
export OPENAI_API_KEY=sk-...
CHRONOS_CONFIG=examples/yaml-configs/graph-agent.yaml \
  go run ./cli/main.go team run classifier-team "What are goroutines?"
```

---

## Pattern 4: Multi-Turn Sessions with Memory

Persistent conversations where the agent remembers previous turns. Uses SQLite to store the full conversation history and automatically summarizes when approaching the context window limit.

### Go Code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
    ctx := context.Background()

    store, _ := sqlite.New("conversations.db")
    defer store.Close()
    store.Migrate(ctx)

    a, err := agent.New("memory-agent", "Memory Agent").
        WithModel(model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("You are a helpful assistant. Remember what the user tells you.").
        WithContextConfig(agent.ContextConfig{
            SummarizeThreshold:  0.8,  // summarize at 80% context window
            PreserveRecentTurns: 5,    // keep last 5 exchanges verbatim
        }).
        Build()
    if err != nil {
        log.Fatal(err)
    }

    sessionID := "session-001"

    // Turn 1: introduce yourself
    resp, _ := a.ChatWithSession(ctx, sessionID, "My name is Alex and I'm learning Go.")
    fmt.Println("Turn 1:", resp.Content)

    // Turn 2: the agent remembers your name
    resp, _ = a.ChatWithSession(ctx, sessionID, "What's my name and what am I learning?")
    fmt.Println("Turn 2:", resp.Content)

    // Turn 3: continue the conversation
    resp, _ = a.ChatWithSession(ctx, sessionID, "What should I learn after goroutines?")
    fmt.Println("Turn 3:", resp.Content)
}
```

### YAML Equivalent

```yaml
agents:
  - id: memory-agent
    name: Memory Agent
    model:
      provider: openai
      model: gpt-4o
      api_key: ${OPENAI_API_KEY}
    storage:
      backend: sqlite
      dsn: conversations.db
    system_prompt: You are a helpful assistant. Remember what the user tells you.
    context:
      summarize_threshold: 0.8
      preserve_recent_turns: 5
```

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go repl
# Now type messages — the agent remembers across turns
```

---

## Pattern 5: Multi-Agent Team (Sequential Pipeline)

Three agents collaborate in a pipeline: one researches, one writes, one edits. Each sees the output of the previous agent.

### Go Code

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/spawn08/chronos/engine/graph"
    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/sdk/team"
)

func main() {
    ctx := context.Background()
    provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

    researcher, _ := agent.New("researcher", "Researcher").
        WithModel(provider).
        WithSystemPrompt("You are a research analyst. Provide 5 key facts with data.").
        Build()

    writer, _ := agent.New("writer", "Writer").
        WithModel(provider).
        WithSystemPrompt("Given research notes, write a 300-word article.").
        Build()

    editor, _ := agent.New("editor", "Editor").
        WithModel(provider).
        WithSystemPrompt("Review and improve the article. Fix grammar, improve flow.").
        Build()

    t := team.New("pipeline", "Content Pipeline", team.StrategySequential)
    t.AddAgent(researcher)
    t.AddAgent(writer)
    t.AddAgent(editor)

    result, err := t.Run(ctx, graph.State{
        "message": "Write about the impact of AI on healthcare",
    })
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result["response"])
}
```

### YAML Equivalent

```yaml
defaults:
  model:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none

agents:
  - id: researcher
    name: Research Analyst
    system_prompt: You are a research analyst. Provide 5 key facts with data.

  - id: writer
    name: Content Writer
    system_prompt: Given research notes, write a 300-word article.

  - id: editor
    name: Senior Editor
    system_prompt: Review and improve the article. Fix grammar, improve flow.

teams:
  - id: pipeline
    name: Content Pipeline
    strategy: sequential
    agents: [researcher, writer, editor]
```

```bash
export OPENAI_API_KEY=sk-...
CHRONOS_CONFIG=examples/yaml-configs/content-pipeline.yaml \
  go run ./cli/main.go team run pipeline "Write about AI in healthcare"
```

---

## Pattern 6: Coordinator Team (Task Decomposition)

A coordinator agent breaks down a complex task and delegates to specialists. The coordinator uses the LLM to plan sub-tasks, assign them, and synthesize results.

### Go Code

```go
ctx := context.Background()
provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

lead, _ := agent.New("lead", "Tech Lead").
    WithModel(provider).
    WithSystemPrompt("Break feature requests into tasks. Assign to backend-dev or frontend-dev.").
    Build()

backend, _ := agent.New("backend", "Backend Dev").
    WithModel(provider).
    WithSystemPrompt("Write clean Go backend code with error handling.").
    Build()

frontend, _ := agent.New("frontend", "Frontend Dev").
    WithModel(provider).
    WithSystemPrompt("Write TypeScript/React code with accessibility.").
    Build()

t := team.New("dev-team", "Dev Team", team.StrategyCoordinator)
t.SetCoordinator(lead)
t.AddAgent(backend)
t.AddAgent(frontend)
t.SetMaxIterations(2)

result, _ := t.Run(ctx, graph.State{
    "message": "Build a user registration feature with email/password",
})
fmt.Println(result["response"])
```

### YAML Equivalent

```yaml
defaults:
  model:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}
  storage:
    backend: none

agents:
  - id: tech-lead
    name: Technical Lead
    system_prompt: Break feature requests into tasks. Assign to backend-dev or frontend-dev.

  - id: backend-dev
    name: Backend Developer
    system_prompt: Write clean Go backend code with error handling.

  - id: frontend-dev
    name: Frontend Developer
    system_prompt: Write TypeScript/React code with accessibility.

teams:
  - id: dev-team
    name: Development Team
    strategy: coordinator
    coordinator: tech-lead
    agents: [backend-dev, frontend-dev]
    max_iterations: 2
```

---

## Pattern 7: Interrupt & Resume (Human-in-the-Loop)

A graph that pauses at a critical decision point, waits for human approval, then resumes.

### Go Code

```go
ctx := context.Background()
provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
store, _ := sqlite.New("approval.db")
defer store.Close()
store.Migrate(ctx)

g := graph.New("approval-workflow")

g.AddNode("analyze", func(ctx context.Context, s graph.State) (graph.State, error) {
    resp, err := provider.Chat(ctx, &model.ChatRequest{
        Messages: []model.Message{
            {Role: model.RoleSystem, Content: "Analyze this request and recommend approve or reject with reasoning."},
            {Role: model.RoleUser, Content: s["request"].(string)},
        },
    })
    if err != nil {
        return s, err
    }
    s["analysis"] = resp.Content
    return s, nil
})

// This node pauses for human review BEFORE executing
g.AddInterruptNode("human_review", func(ctx context.Context, s graph.State) (graph.State, error) {
    s["reviewed"] = true
    s["status"] = "approved"
    return s, nil
})

g.AddNode("execute", func(ctx context.Context, s graph.State) (graph.State, error) {
    resp, err := provider.Chat(ctx, &model.ChatRequest{
        Messages: []model.Message{
            {Role: model.RoleSystem, Content: "Execute the approved request. Confirm what was done."},
            {Role: model.RoleUser, Content: fmt.Sprintf("Request: %s\nAnalysis: %s", s["request"], s["analysis"])},
        },
    })
    if err != nil {
        return s, err
    }
    s["result"] = resp.Content
    return s, nil
})

g.SetEntryPoint("analyze")
g.AddEdge("analyze", "human_review")
g.AddEdge("human_review", "execute")
g.SetFinishPoint("execute")

a, _ := agent.New("approval-agent", "Approval Agent").
    WithStorage(store).
    WithGraph(g).
    Build()

// Start the workflow — it will pause at human_review
result, _ := a.Run(ctx, map[string]any{
    "request": "Deploy version 2.0 to production",
})
fmt.Printf("Status: %s (paused for review)\n", result.Status)
fmt.Printf("Analysis: %s\n", result.State["analysis"])

// Later: resume after human approves
result, _ = a.Resume(ctx, result.SessionID)
fmt.Printf("Status: %s\n", result.Status)
fmt.Printf("Result: %s\n", result.State["result"])
```

---

## Pattern 8: Fallback Provider (High Availability)

Chain multiple providers for automatic failover. If OpenAI is down, fall back to Anthropic, then to local Ollama.

### Go Code

```go
primary := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))
secondary := model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))
local := model.NewOllama("http://localhost:11434", "llama3.2")

provider, _ := model.NewFallbackProvider(primary, secondary, local)
provider.OnFallback = func(index int, name string, err error) {
    log.Printf("Provider %s failed (index %d): %v", name, index, err)
}

a, _ := agent.New("ha-agent", "HA Agent").
    WithModel(provider).
    WithSystemPrompt("You are a helpful assistant.").
    Build()

resp, _ := a.Chat(ctx, "Hello!")
fmt.Println(resp.Content)
```

---

## Pattern 9: Streaming Responses

Get token-by-token streaming output from any provider.

### Go Code

```go
provider := model.NewOpenAI(os.Getenv("OPENAI_API_KEY"))

ch, err := provider.StreamChat(ctx, &model.ChatRequest{
    Messages: []model.Message{
        {Role: model.RoleUser, Content: "Write a haiku about programming."},
    },
    Stream: true,
})
if err != nil {
    log.Fatal(err)
}

for resp := range ch {
    fmt.Print(resp.Content) // prints token by token
}
fmt.Println()
```

---

## Pattern 10: Deploy via CLI (No Go Code)

Use the `chronos deploy` command to run agents from a YAML config in a sandboxed environment.

### Deployment YAML

```yaml
# deploy-config.yaml
name: coding-team-deploy
sandbox:
  backend: process
  work_dir: /tmp/chronos-sandbox

defaults:
  model:
    provider: openai
    model: gpt-4o
    api_key: ${OPENAI_API_KEY}

agents:
  - id: planner
    name: Planner
    system_prompt: Break tasks into sub-tasks with clear acceptance criteria.
    tools:
      - name: shell_auto
      - name: file_read

  - id: coder
    name: Coder
    system_prompt: Implement code based on the plan. Write clean, tested code.
    tools:
      - name: shell_auto
      - name: file_read
      - name: file_write

teams:
  - id: dev-team
    name: Development Team
    strategy: sequential
    agents: [planner, coder]
```

### Run It

```bash
export OPENAI_API_KEY=sk-...
go run ./cli/main.go deploy deploy-config.yaml "Create a hello world HTTP server in Go"
```

---

## Full Environment Variable Reference

| Variable | Required By | Purpose |
|----------|------------|---------|
| `OPENAI_API_KEY` | OpenAI, GPT models | Authentication |
| `ANTHROPIC_API_KEY` | Anthropic, Claude models | Authentication |
| `GEMINI_API_KEY` | Google Gemini | Authentication |
| `MISTRAL_API_KEY` | Mistral AI | Authentication |
| `GROQ_API_KEY` | Groq | Authentication |
| `DEEPSEEK_API_KEY` | DeepSeek | Authentication |
| `AZURE_OPENAI_API_KEY` | Azure OpenAI | Authentication |
| `AZURE_OPENAI_ENDPOINT` | Azure OpenAI | Resource endpoint URL |
| `AZURE_OPENAI_DEPLOYMENT` | Azure OpenAI | Model deployment name |
| `AZURE_OPENAI_API_VERSION` | Azure OpenAI | API version (default: `2024-10-21`) |
| `CHRONOS_CONFIG` | CLI | Path to YAML config file |
| `CHRONOS_DB_PATH` | CLI | SQLite database path (default: `chronos.db`) |
| `CHRONOS_API_KEY` | CLI | Default provider API key |
| `CHRONOS_MODEL` | CLI | Default model (default: `gpt-4o`) |

---

## Next Steps

- [Model Providers](/guides/models/) — All 14+ supported providers with configuration
- [StateGraph Runtime](/guides/stategraph/) — Deep dive into graph patterns
- [Tools & Function Calling](/guides/tools/) — Built-in and custom tools
- [Multi-Agent Teams](/guides/teams/) — All team strategies with examples
- [YAML Examples](/guides/yaml-examples/) — 5 ready-to-run YAML configurations
- [Storage Adapters](/guides/storage/) — SQLite, PostgreSQL, Redis, and more
