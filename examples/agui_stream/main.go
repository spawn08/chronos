// Example: agui_stream shows Chronos runs surfaced as a standard AG-UI event
// stream (WC-B-003). It starts the AG-UI SSE handler over a stream.Broker,
// connects a client, replays a scripted run (steps, a tool call, a plan update,
// completion) into the broker, and prints the AG-UI events a compatible frontend
// would render — no LLM or API key required.
//
//	go run ./examples/agui_stream/
package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/os/interop/agui"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║        Chronos AG-UI Event Stream Example              ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	broker := stream.NewBroker()
	defer broker.Close()

	ts := httptest.NewServer(agui.Handler(broker))
	defer ts.Close()

	const session = "demo-session"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"?session="+session+"&run=demo-run", http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	// Replay a run into the broker once the client is subscribed.
	go func() {
		time.Sleep(50 * time.Millisecond)
		pub := func(t string, d map[string]any) { broker.PublishTopic(session, stream.Event{Type: t, Data: d}) }
		pub(stream.EventNodeStart, map[string]any{"node_id": "research"})
		pub(stream.EventToolCall, map[string]any{"tool": "web_search", "args": map[string]any{"q": "Go history"}})
		pub(stream.EventToolResult, map[string]any{"tool": "web_search", "result": "Go: 2009, Google"})
		pub(stream.EventNodeEnd, map[string]any{"node_id": "research"})
		pub(stream.EventPlanUpdate, map[string]any{"summary": "[x] research\n[~] write", "complete": false})
		// Streamed assistant text: deltas open a TEXT_MESSAGE; the response closes it.
		pub(stream.EventModelDelta, map[string]any{"content": "Go was "})
		pub(stream.EventModelDelta, map[string]any{"content": "released in 2009."})
		pub(stream.EventModelResponse, map[string]any{"stop_reason": "end"})
		pub(stream.EventCompleted, map[string]any{})
	}()

	fmt.Printf("\nAG-UI events for %q:\n\n", session)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data: ")
		if !ok {
			continue
		}
		fmt.Printf("  %s\n", data)
		if strings.Contains(data, string(agui.EventRunFinished)) {
			break
		}
	}
	fmt.Println("\n✓ A standard AG-UI frontend renders this run with no Chronos-specific glue.")
}
