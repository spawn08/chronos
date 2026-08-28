---
title: "Webhooks"
---


The `os/webhook` package provides a generic inbound webhook server for receiving external events — GitHub deliveries, payment provider callbacks, CI notifications, or any other system that pushes JSON over HTTP. It routes incoming requests to registered handlers by event type and can gate access with a shared secret.

The snippets on this page assume these imports:

```go
import (
    "context"
    "fmt"
    "log"
    "net/http"

    "github.com/spawn08/chronos/os/webhook"
)
```

## Creating a server

`webhook.NewServer` takes an optional shared secret. Pass `""` to disable secret validation.

```go
srv := webhook.NewServer("my-webhook-secret")
```

Internally, `NewServer` builds its own `*http.ServeMux` and registers both `/webhook` and `/webhook/` (so subpaths like `/webhook/github` also route to the handler).

## Registering handlers

Register a `Handler` for a specific event type with `On`. Multiple handlers can be registered for the same event type — all of them run for each matching event.

```go
type Handler func(ctx context.Context, event Event) error

type Event struct {
    Source  string            `json:"source"`
    Type    string            `json:"type"`
    Body    json.RawMessage   `json:"body"`
    Headers map[string]string `json:"headers,omitempty"`
}
```

```go
srv.On("deployment", func(ctx context.Context, e webhook.Event) error {
    log.Printf("deployment event from %s: %s", e.Source, string(e.Body))
    return nil
})

// "*" registers a wildcard handler that runs for every event type,
// in addition to any type-specific handlers.
srv.On("*", func(ctx context.Context, e webhook.Event) error {
    log.Printf("event received: type=%s source=%s", e.Type, e.Source)
    return nil
})
```

If a handler returns an error, the server responds `500 Internal Server Error` and includes the aggregated handler errors in the response body. If every handler for the event succeeds, the server responds `200 OK` with `{"status":"ok"}`.

### How events are built

For each incoming `POST` request, the server reads up to 1 MiB of the body and builds an `Event`:

- **`Type`** comes from the `X-Event-Type` request header. If the header is absent, the type defaults to `"generic"`.
- **`Source`** comes from the `X-Event-Source` request header.
- **`Body`** is the raw request body, exposed as `json.RawMessage` so handlers can unmarshal it into whatever shape they expect.
- **`Headers`** contains every header from the incoming request.

Only `POST` requests are accepted; any other method gets `405 Method Not Allowed`.

## Mounting the server

`Server.Handler()` returns an `http.Handler` you can mount directly, or run standalone:

```go
func main() {
    srv := webhook.NewServer("my-webhook-secret")

    srv.On("payment.succeeded", func(ctx context.Context, e webhook.Event) error {
        fmt.Println("payment succeeded:", string(e.Body))
        return nil
    })

    log.Fatal(http.ListenAndServe(":8090", srv.Handler()))
}
```

Because `Handler()` returns a plain `http.Handler`, it can also be mounted under an existing mux alongside other routes (for example, on a subpath of your own `http.ServeMux` or a ChronosOS-embedded server — see [The ChronosOS Server](/guides/server)).

## Securing the endpoint

Passing a non-empty secret to `NewServer` enables validation on every request: the server compares the incoming `X-Webhook-Secret` header against the configured secret and responds `401 Unauthorized` if it doesn't match.

```go
srv := webhook.NewServer("my-webhook-secret")
```

```bash
curl -X POST http://localhost:8090/webhook \
  -H "X-Event-Type: deployment" \
  -H "X-Event-Source: ci" \
  -H "X-Webhook-Secret: my-webhook-secret" \
  -d '{"status":"ok"}'
```

Notes on this mechanism:

- The check is a direct header comparison against the configured secret — not an HMAC-signed payload. If the sender you're integrating with supports signing (e.g. an `X-Hub-Signature` style header), verify that signature yourself inside your handler before trusting `event.Body`; the raw headers are available on `event.Headers` for exactly this purpose.
- Always terminate the webhook endpoint behind TLS in production so the shared secret isn't sent in plaintext.
- Leave the secret as `""` only for local development or trusted internal networks.

## Complete example

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"

    "github.com/spawn08/chronos/os/webhook"
)

type deployPayload struct {
    Service string `json:"service"`
    Status  string `json:"status"`
}

func main() {
    srv := webhook.NewServer("my-webhook-secret")

    srv.On("deployment", func(ctx context.Context, e webhook.Event) error {
        var p deployPayload
        if err := json.Unmarshal(e.Body, &p); err != nil {
            return fmt.Errorf("invalid deployment payload: %w", err)
        }
        log.Printf("deployment for %s is now %s", p.Service, p.Status)
        return nil
    })

    srv.On("*", func(ctx context.Context, e webhook.Event) error {
        log.Printf("webhook received: type=%s source=%s", e.Type, e.Source)
        return nil
    })

    log.Println("listening on :8090")
    log.Fatal(http.ListenAndServe(":8090", srv.Handler()))
}
```
