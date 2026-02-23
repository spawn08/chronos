Create a new guardrail for Chronos agent execution.

The guardrail name/purpose is: $ARGUMENTS

## Instructions

1. Implement the `guardrails.Guardrail` interface from `engine/guardrails/guardrails.go`:
```go
type Guardrail interface {
    Check(ctx context.Context, content string) Result
}
```

2. `Result` has `Passed bool` and `Reason string`

3. Example implementation:
```go
type MyGuardrail struct {
    // config fields
}

func (g *MyGuardrail) Check(_ context.Context, content string) guardrails.Result {
    if /* violation detected */ {
        return guardrails.Result{Passed: false, Reason: "why it failed"}
    }
    return guardrails.Result{Passed: true}
}
```

4. Register on an agent via the builder:
```go
agent.New("id", "name").
    AddInputGuardrail("name", &MyGuardrail{}).   // checks user input
    AddOutputGuardrail("name", &MyGuardrail{})    // checks model output
```

5. Built-in guardrails available: `BlocklistGuardrail`, `MaxLengthGuardrail`
6. Guardrails are checked in order; first failure blocks the request
7. Run `go build ./...` to verify
