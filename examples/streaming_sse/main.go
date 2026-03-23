// Example: streaming_sse demonstrates the event streaming broker for real-time observability.
//
// This shows how to use the SSE broker for publishing events, subscribing to
// event streams, and serving SSE over HTTP — useful for dashboards, monitoring,
// and real-time agent observability.
//
// No API keys needed.
//
//	go run ./examples/streaming_sse/
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/spawn08/chronos/engine/graph"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Streaming & SSE Example                   ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	// ── 1. Basic pub/sub with the event broker ──

	fmt.Println("\n━━━ 1. Event Broker (Pub/Sub) ━━━")

	broker := stream.NewBroker()

	sub1 := broker.Subscribe("dashboard")
	sub2 := broker.Subscribe("logger")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		count := 0
		for evt := range sub1 {
			count++
			fmt.Printf("  [dashboard] type=%-20s data=%v\n", evt.Type, evt.Data)
			if count >= 4 {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		count := 0
		for evt := range sub2 {
			count++
			fmt.Printf("  [logger]    type=%-20s data=%v\n", evt.Type, evt.Data)
			if count >= 4 {
				return
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	broker.Publish(stream.Event{
		Type: "agent.started",
		Data: map[string]any{"agent_id": "researcher", "timestamp": time.Now().Format(time.RFC3339)},
	})
	broker.Publish(stream.Event{
		Type: "model.call",
		Data: map[string]any{"model": "gpt-4o", "tokens": 150},
	})
	broker.Publish(stream.Event{
		Type: "tool.executed",
		Data: map[string]any{"tool": "calculate", "args": map[string]any{"op": "add", "a": 1, "b": 2}},
	})
	broker.Publish(stream.Event{
		Type: "agent.completed",
		Data: map[string]any{"agent_id": "researcher", "duration_ms": 1200},
	})

	time.Sleep(100 * time.Millisecond)
	broker.Unsubscribe("dashboard")
	broker.Unsubscribe("logger")
	wg.Wait()

	// ── 2. Graph runner stream events ──

	fmt.Println("\n━━━ 2. Graph Runner Stream Events ━━━")

	ctx := context.Background()
	store, err := sqlite.New(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		log.Fatal(err)
	}

	etlGraph := graph.New("etl-pipeline").
		AddNode("extract", func(_ context.Context, s graph.State) (graph.State, error) {
			s["extracted"] = 500
			return s, nil
		}).
		AddNode("transform", func(_ context.Context, s graph.State) (graph.State, error) {
			count, _ := s["extracted"].(int)
			s["transformed"] = count - 3
			s["errors"] = 3
			return s, nil
		}).
		AddNode("load", func(_ context.Context, s graph.State) (graph.State, error) {
			s["loaded"] = true
			s["target"] = "data_warehouse"
			return s, nil
		}).
		SetEntryPoint("extract").
		AddEdge("extract", "transform").
		AddEdge("transform", "load").
		SetFinishPoint("load")

	compiled, err := etlGraph.Compile()
	if err != nil {
		log.Fatal(err)
	}

	runner := graph.NewRunner(compiled, store)

	var streamWg sync.WaitGroup
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		for evt := range runner.Stream() {
			fmt.Printf("  [GRAPH EVENT] type=%-16s node=%-12s time=%s\n",
				evt.Type, evt.NodeID, evt.Timestamp.Format("15:04:05.000"))
		}
	}()

	result, err := runner.Run(ctx, "etl-session", graph.State{"source": "csv_files"})
	if err != nil {
		log.Fatal(err)
	}
	streamWg.Wait()

	fmt.Printf("  Pipeline result: extracted=%v  transformed=%v  loaded=%v\n",
		result.State["extracted"], result.State["transformed"], result.State["loaded"])

	// ── 3. SSE HTTP handler ──

	fmt.Println("\n━━━ 3. SSE HTTP Handler (Server-Sent Events) ━━━")

	sseBroker := stream.NewBroker()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", sseBroker.SSEHandler("http-client"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	serverAddr := ln.Addr().String()

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(ln) }()
	defer server.Close()

	fmt.Printf("  SSE server listening on http://%s/events\n", serverAddr)
	fmt.Println("  (In production, connect with EventSource or curl --no-buffer)")

	go func() {
		time.Sleep(100 * time.Millisecond)
		sseBroker.Publish(stream.Event{Type: "heartbeat", Data: "ok"})
		sseBroker.Publish(stream.Event{Type: "agent.update", Data: map[string]any{"status": "running"}})
		time.Sleep(50 * time.Millisecond)
		sseBroker.Unsubscribe("http-client")
	}()

	time.Sleep(300 * time.Millisecond)

	fmt.Println("  SSE events published successfully")

	fmt.Println("\n✓ Streaming & SSE example completed.")
}
