Create a new execution hook for Chronos.

The hook name/purpose is: $ARGUMENTS

## Instructions

1. Implement the `hooks.Hook` interface from `engine/hooks/hooks.go`:
```go
type Hook interface {
    Before(ctx context.Context, evt *Event) error  // return error to abort
    After(ctx context.Context, evt *Event) error
}
```

2. `Event` has: `Type` (EventType), `Name`, `Input`, `Output`, `Error`, `Metadata`

3. Available event types:
   - `hooks.EventToolCallBefore` / `hooks.EventToolCallAfter`
   - `hooks.EventModelCallBefore` / `hooks.EventModelCallAfter`
   - `hooks.EventNodeBefore` / `hooks.EventNodeAfter`

4. Example implementation:
```go
type MetricsHook struct {
    // fields
}

func (h *MetricsHook) Before(ctx context.Context, evt *hooks.Event) error {
    // record start time, log, etc.
    return nil
}

func (h *MetricsHook) After(ctx context.Context, evt *hooks.Event) error {
    // record duration, emit metrics, etc.
    return nil
}
```

5. Register on agent via builder: `.AddHook(&MetricsHook{})`
6. Hooks run in order (Before) and reverse order (After) — like middleware
7. Returning an error from `Before` aborts the operation
8. See `hooks.LoggingHook` for a reference implementation
9. Run `go build ./...` to verify
