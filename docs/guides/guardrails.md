---
title: "Guardrails"
permalink: /guides/guardrails/
sidebar:
  nav: "docs"
toc: true
toc_sticky: true
---

Guardrails validate input and output at runtime. They block content that violates policy before it reaches the model or after the model responds. Use them to enforce blocklists, length limits, PII filtering, and custom rules.

## Guardrail Interface

Every guardrail implements the `Guardrail` interface:

```go
type Guardrail interface {
    Check(ctx context.Context, content string) Result
}

type Result struct {
    Passed  bool   `json:"passed"`
    Reason  string `json:"reason,omitempty"`
}
```

- **Passed**: `true` if the content is acceptable; `false` to block.
- **Reason**: Human-readable explanation when `Passed` is `false`.

## Engine

The guardrails engine runs rules by position (input or output):

```go
engine := guardrails.NewEngine()
engine.AddRule(guardrails.Rule{
    Name:      "blocklist",
    Position:  guardrails.Input,
    Guardrail: &guardrails.BlocklistGuardrail{Blocklist: []string{"spam"}},
})
```

- **CheckInput(ctx, content)**: Runs all input guardrails. Returns the first failure, or nil.
- **CheckOutput(ctx, content)**: Runs all output guardrails. Returns the first failure, or nil.

## Position

| Position | When Checked |
|----------|--------------|
| `Input` | Before the model receives user content |
| `Output` | After the model produces a response |

## Built-in Guardrails

### BlocklistGuardrail

Rejects content containing any blocked term (case-insensitive).

```go
g := &guardrails.BlocklistGuardrail{
    Blocklist: []string{"password", "secret", "confidential"},
}
result := g.Check(ctx, "Please share your password")
// result.Passed == false, result.Reason == "blocked term: \"password\""
```

### MaxLengthGuardrail

Rejects content exceeding a character limit.

```go
g := &guardrails.MaxLengthGuardrail{MaxChars: 1000}
result := g.Check(ctx, longString)
// result.Passed == false if len(longString) > 1000
```

## Adding via Builder

Use the agent builder to attach guardrails:

```go
a, err := agent.New("my-agent", "My Agent").
    WithModel(provider).
    AddInputGuardrail("blocklist", &guardrails.BlocklistGuardrail{
        Blocklist: []string{"spam", "malware"},
    }).
    AddOutputGuardrail("max_length", &guardrails.MaxLengthGuardrail{
        MaxChars: 4000,
    }).
    Build()
```

- **AddInputGuardrail(name, g)**: Adds input validation.
- **AddOutputGuardrail(name, g)**: Adds output validation.

## Where Guardrails Run

| Method | Input Checked | Output Checked |
|--------|---------------|----------------|
| `Chat` | `userMessage` before model call | `resp.Content` after model response |
| `ChatWithSession` | `userMessage` before model call | `resp.Content` after model response |
| `Run` | `input["message"]` if present | Not applicable |

When a guardrail fails, the method returns an error: `"input guardrail failed: [input] blocklist: blocked term: \"...\""` or `"output guardrail failed: ..."`.

## Custom Guardrails

Implement the `Guardrail` interface for custom logic:

```go
type PIIGuardrail struct {
    Patterns []*regexp.Regexp
}

func (g *PIIGuardrail) Check(_ context.Context, content string) guardrails.Result {
    for _, p := range g.Patterns {
        if p.MatchString(content) {
            return guardrails.Result{
                Passed: false,
                Reason: fmt.Sprintf("PII detected: %s", p.String()),
            }
        }
    }
    return guardrails.Result{Passed: true}
}
```

Register it:

```go
a.AddInputGuardrail("pii", &PIIGuardrail{
    Patterns: []*regexp.Regexp{
        regexp.MustCompile(`\b\d{16}\b`),           // credit card
        regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`), // SSN
    },
})
```

## Example: Building a PII Guardrail

```go
package main

import (
    "context"
    "fmt"
    "regexp"

    "github.com/chronos-ai/chronos/engine/guardrails"
    "github.com/chronos-ai/chronos/engine/model"
    "github.com/chronos-ai/chronos/sdk/agent"
)

type PIIGuardrail struct {
    Patterns []struct {
        Name    string
        Pattern *regexp.Regexp
    }
}

func NewPIIGuardrail() *PIIGuardrail {
    return &PIIGuardrail{
        Patterns: []struct {
            Name    string
            Pattern *regexp.Regexp
        }{
            {"credit_card", regexp.MustCompile(`\b\d{4}[\s-]?\d{4}[\s-]?\d{4}[\s-]?\d{4}\b`)},
            {"ssn", regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)},
            {"email", regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b`)},
        },
    }
}

func (g *PIIGuardrail) Check(_ context.Context, content string) guardrails.Result {
    for _, p := range g.Patterns {
        if p.Pattern.MatchString(content) {
            return guardrails.Result{
                Passed: false,
                Reason: fmt.Sprintf("PII pattern matched: %s", p.Name),
            }
        }
    }
    return guardrails.Result{Passed: true}
}

func main() {
    ctx := context.Background()
    provider := model.NewOpenAI("your-api-key")

    a, err := agent.New("safe-agent", "Safe Agent").
        WithModel(provider).
        WithSystemPrompt("You are a helpful assistant.").
        AddInputGuardrail("pii", NewPIIGuardrail()).
        AddOutputGuardrail("max_length", &guardrails.MaxLengthGuardrail{MaxChars: 2000}).
        Build()
    if err != nil {
        panic(err)
    }

    resp, err := a.Chat(ctx, "What is the capital of France?")
    if err != nil {
        fmt.Println("Error:", err)
        return
    }
    fmt.Println(resp.Content)
}
```
