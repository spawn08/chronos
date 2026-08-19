Embed Chronos agents into an existing Go HTTP service.

The service/integration is: $ARGUMENTS

## Instructions

1. Read the agent builder at `sdk/agent/agent.go`, the streaming broker at `engine/stream/`, and the ChronosOS server at `os/server.go` for patterns.

2. Choose the integration pattern:

   | Pattern | Use Case |
   |---------|----------|
   | **Embedded agent** | Agent runs inside your existing HTTP server |
   | **Agent as middleware** | Process requests through an agent before your handlers |
   | **Streaming endpoint** | SSE streaming for real-time agent responses |
   | **Background worker** | Agent processes tasks from a queue |

---

### Pattern 1: Embedded Agent in HTTP Server

3. Add Chronos as a dependency:
```bash
go get github.com/spawn08/chronos
```

4. Create an agent and expose it via your API:

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"

    "github.com/spawn08/chronos/engine/model"
    "github.com/spawn08/chronos/sdk/agent"
    "github.com/spawn08/chronos/storage/adapters/sqlite"
)

type ChatRequest struct {
    Message   string `json:"message"`
    SessionID string `json:"session_id,omitempty"`
}

type ChatResponse struct {
    Response  string `json:"response"`
    SessionID string `json:"session_id"`
}

func main() {
    ctx := context.Background()

    // Initialize storage
    store, err := sqlite.New("app.db")
    if err != nil { log.Fatal(err) }
    defer store.Close()
    store.Migrate(ctx)

    // Build the agent once at startup
    a, err := agent.New("assistant", "Assistant").
        WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("You are a helpful assistant for our product.").
        Build()
    if err != nil { log.Fatal(err) }

    // HTTP handler
    http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
            return
        }

        var req ChatRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid request", http.StatusBadRequest)
            return
        }

        result, err := a.Run(r.Context(), req.Message)
        if err != nil {
            http.Error(w, "agent error", http.StatusInternalServerError)
            return
        }

        resp := ChatResponse{
            Response:  result.Output,
            SessionID: result.SessionID,
        }
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    })

    log.Println("Server running on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}
```

---

### Pattern 2: Streaming SSE Endpoint

5. For real-time streaming responses:

```go
import (
    "github.com/spawn08/chronos/engine/stream"
)

func streamHandler(a *agent.Agent) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        flusher, ok := w.(http.Flusher)
        if !ok {
            http.Error(w, "streaming not supported", http.StatusInternalServerError)
            return
        }

        var req ChatRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid request", http.StatusBadRequest)
            return
        }

        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")

        // Create a stream broker
        broker := stream.NewBroker()
        ch := broker.Subscribe()
        defer broker.Unsubscribe(ch)

        // Run agent with streaming
        go func() {
            a.RunStream(r.Context(), req.Message, broker)
        }()

        for event := range ch {
            data, _ := json.Marshal(event)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()
        }
    }
}

// Register: http.HandleFunc("/api/chat/stream", streamHandler(a))
```

---

### Pattern 3: Agent as Middleware

6. Process every request through an agent before your handler:

```go
func agentMiddleware(a *agent.Agent, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract intent from the request
        query := r.URL.Query().Get("q")
        if query != "" {
            result, err := a.Run(r.Context(), query)
            if err == nil {
                // Inject agent output into request context
                ctx := context.WithValue(r.Context(), "agent_result", result)
                r = r.WithContext(ctx)
            }
        }
        next.ServeHTTP(w, r)
    })
}
```

---

### Pattern 4: Background Worker

7. Process tasks from a queue using an agent:

```go
func worker(ctx context.Context, a *agent.Agent, tasks <-chan string) {
    for {
        select {
        case <-ctx.Done():
            return
        case task := <-tasks:
            result, err := a.Run(ctx, task)
            if err != nil {
                log.Printf("task failed: %v", err)
                continue
            }
            log.Printf("task completed: session=%s", result.SessionID)
        }
    }
}

// Usage:
// tasks := make(chan string, 100)
// go worker(ctx, a, tasks)
// tasks <- "Process this document..."
```

---

### Pattern 5: Multi-Agent Service

8. Run multiple agents in the same service:

```go
func main() {
    ctx := context.Background()
    store, _ := sqlite.New("multi.db")
    store.Migrate(ctx)

    // Build specialized agents
    classifier, _ := agent.New("classifier", "Classifier").
        WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("Classify the user's intent into: question, complaint, feedback").
        Build()

    responder, _ := agent.New("responder", "Responder").
        WithModel(model.NewAnthropic(os.Getenv("ANTHROPIC_API_KEY"))).
        WithStorage(store).
        WithSystemPrompt("Respond helpfully to the user's message").
        Build()

    http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
        var req ChatRequest
        json.NewDecoder(r.Body).Decode(&req)

        // Step 1: Classify
        classification, _ := classifier.Run(r.Context(), req.Message)

        // Step 2: Route to appropriate handler based on classification
        result, _ := responder.Run(r.Context(), req.Message)

        json.NewEncoder(w).Encode(ChatResponse{
            Response:  result.Output,
            SessionID: result.SessionID,
        })
    })
}
```

---

### YAML + ChronosOS Hybrid

9. If you prefer YAML-defined agents with the full ChronosOS control plane:

```go
import (
    chronosOS "github.com/spawn08/chronos/os"
)

func main() {
    // Load agents from YAML and start ChronosOS alongside your app
    server, err := chronosOS.NewServer(chronosOS.Config{
        ConfigPath: "agents.yaml",
        Addr:       ":8420",
        Auth:       true,
    })
    if err != nil { log.Fatal(err) }

    // Mount ChronosOS under a prefix in your existing router
    mux := http.NewServeMux()
    mux.Handle("/chronos/", http.StripPrefix("/chronos", server.Handler()))
    mux.HandleFunc("/api/your-app", yourHandler)

    log.Fatal(http.ListenAndServe(":8080", mux))
}
```

10. Run `go build ./...` to verify compilation.
