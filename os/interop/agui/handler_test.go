package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/stream"
)

// readAGUI opens the SSE handler for a session and returns a function that reads
// the next AG-UI event (blocking until one arrives or the deadline passes).
func readAGUI(t *testing.T, ts *httptest.Server, session string) (next func() (Event, bool), stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"?session="+session, http.NoBody)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("connect %s: %v", session, err)
	}
	// Headers received => the handler has subscribed; publishing now is safe.
	scanner := bufio.NewScanner(resp.Body)
	next = func() (Event, bool) {
		for scanner.Scan() {
			line := scanner.Text()
			data, ok := strings.CutPrefix(line, "data: ")
			if !ok {
				continue // heartbeat / blank line
			}
			var e Event
			if json.Unmarshal([]byte(data), &e) == nil {
				return e, true
			}
		}
		return Event{}, false
	}
	return next, func() { cancel(); resp.Body.Close() }
}

func TestHandler_StreamsTranslatedRun(t *testing.T) {
	broker := stream.NewBroker()
	defer broker.Close()
	ts := httptest.NewServer(Handler(broker))
	defer ts.Close()

	next, stop := readAGUI(t, ts, "s1")
	defer stop()

	// The first event is always RUN_STARTED.
	if e, ok := next(); !ok || e.Type != EventRunStarted {
		t.Fatalf("first event = %+v (ok=%v), want RUN_STARTED", e, ok)
	}

	// Publish a tool call and completion to this session; expect translated events.
	go func() {
		time.Sleep(10 * time.Millisecond)
		broker.PublishTopic("s1", stream.Event{Type: stream.EventToolCall, Data: map[string]any{"tool": "calc", "args": map[string]any{"x": 1}}})
		broker.PublishTopic("s1", stream.Event{Type: stream.EventCompleted, Data: map[string]any{}})
	}()

	var seen []EventType
	deadline := time.After(3 * time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			e, ok := next()
			if !ok {
				return
			}
			seen = append(seen, e.Type)
			if e.Type == EventRunFinished {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatalf("did not observe RUN_FINISHED; saw %v", seen)
	}

	want := map[EventType]bool{EventToolCallStart: false, EventToolCallEnd: false, EventRunFinished: false}
	for _, ty := range seen {
		if _, tracked := want[ty]; tracked {
			want[ty] = true
		}
	}
	for ty, got := range want {
		if !got {
			t.Errorf("missing expected AG-UI event %s; saw %v", ty, seen)
		}
	}
}

// Events published to one session must not appear on another session's stream.
func TestHandler_PerSessionIsolation(t *testing.T) {
	broker := stream.NewBroker()
	defer broker.Close()
	ts := httptest.NewServer(Handler(broker))
	defer ts.Close()

	next2, stop2 := readAGUI(t, ts, "s2")
	defer stop2()
	// Drain s2's RUN_STARTED.
	if e, ok := next2(); !ok || e.Type != EventRunStarted {
		t.Fatalf("s2 first = %+v", e)
	}

	// Publish only to s1.
	broker.PublishTopic("s1", stream.Event{Type: stream.EventCompleted, Data: map[string]any{}})

	// s2 must not receive s1's RUN_FINISHED. Read with a short deadline; anything
	// that arrives must not be the leaked completion.
	type result struct {
		e  Event
		ok bool
	}
	ch := make(chan result, 1)
	go func() { e, ok := next2(); ch <- result{e, ok} }()
	select {
	case r := <-ch:
		if r.ok && r.e.Type == EventRunFinished {
			t.Error("s2 received s1's RUN_FINISHED — cross-session leak")
		}
	case <-time.After(300 * time.Millisecond):
		// No event on s2 within the window: correct isolation.
	}
}
