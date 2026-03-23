package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewBroker(t *testing.T) {
	b := NewBroker()
	if b == nil {
		t.Fatal("NewBroker returned nil")
	}
	if b.clients == nil {
		t.Fatal("clients map should be initialized")
	}
}

func TestBroker_SubscribeAndPublish(t *testing.T) {
	b := NewBroker()
	ch := b.Subscribe("sub-1")

	go func() {
		time.Sleep(10 * time.Millisecond)
		b.Publish(Event{Type: EventNodeStart, Data: map[string]any{"node": "test"}})
	}()

	select {
	case evt := <-ch:
		if evt.Type != EventNodeStart {
			t.Errorf("Type = %q, want %q", evt.Type, EventNodeStart)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestBroker_Unsubscribe(t *testing.T) {
	b := NewBroker()
	ch := b.Subscribe("sub-2")

	b.Unsubscribe("sub-2")

	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestBroker_UnsubscribeNonExistent(t *testing.T) {
	b := NewBroker()
	b.Unsubscribe("non-existent") // should not panic
}

func TestBroker_MultipleSubscribers(t *testing.T) {
	b := NewBroker()
	ch1 := b.Subscribe("s1")
	ch2 := b.Subscribe("s2")
	ch3 := b.Subscribe("s3")

	b.Publish(Event{Type: EventCompleted, Data: "done"})

	for _, ch := range []<-chan Event{ch1, ch2, ch3} {
		select {
		case evt := <-ch:
			if evt.Type != EventCompleted {
				t.Errorf("Type = %q, want %q", evt.Type, EventCompleted)
			}
		case <-time.After(time.Second):
			t.Error("timed out waiting for event on subscriber")
		}
	}

	b.Unsubscribe("s1")
	b.Unsubscribe("s2")
	b.Unsubscribe("s3")
}

func TestBroker_PublishDropsOnFullBuffer(t *testing.T) {
	b := NewBroker()
	ch := b.Subscribe("slow-sub")

	// Fill the buffer (capacity 64)
	for i := 0; i < 70; i++ {
		b.Publish(Event{Type: "fill", Data: i})
	}

	// Channel should have 64 events (buffer size), extras dropped
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 64 {
		t.Errorf("expected 64 buffered events, got %d", count)
	}

	b.Unsubscribe("slow-sub")
}

func TestBroker_ConcurrentPublish(t *testing.T) {
	b := NewBroker()
	ch := b.Subscribe("conc-sub")
	defer b.Unsubscribe("conc-sub")

	const publishers = 10
	const eventsPerPublisher = 5
	var wg sync.WaitGroup
	wg.Add(publishers)

	for i := 0; i < publishers; i++ {
		go func(pub int) {
			defer wg.Done()
			for j := 0; j < eventsPerPublisher; j++ {
				b.Publish(Event{Type: "concurrent", Data: map[string]any{"pub": pub, "seq": j}})
			}
		}(i)
	}

	wg.Wait()

	received := 0
	for {
		select {
		case <-ch:
			received++
		default:
			goto check
		}
	}
check:
	expected := publishers * eventsPerPublisher
	if received > 64 {
		received = 64 // capped by buffer
	}
	if received == 0 {
		t.Error("expected at least some events from concurrent publishers")
	}
	_ = expected
}

func TestBroker_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	b := NewBroker()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			subID := "sub-" + strings.Repeat("x", id%5)
			b.Subscribe(subID)
			b.Publish(Event{Type: "test"})
			b.Unsubscribe(subID)
		}(i)
	}

	wg.Wait() // should not panic or deadlock
}

func TestBroker_SSEHandler(t *testing.T) {
	b := NewBroker()

	handler := b.SSEHandler("sse-sub")

	req := httptest.NewRequest(http.MethodGet, "/events", http.NoBody)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(w, req)
	}()

	// Wait for subscription to be set up
	time.Sleep(50 * time.Millisecond)

	b.Publish(Event{Type: EventModelCall, Data: map[string]any{"model": "test"}})

	// Let the handler process the event
	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	resp := w.Body.String()
	if !strings.Contains(resp, "event: model_call") {
		t.Errorf("SSE response missing event type; got: %q", resp)
	}
	if !strings.Contains(resp, "data:") {
		t.Errorf("SSE response missing data; got: %q", resp)
	}

	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/event-stream")
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

func TestEventConstants(t *testing.T) {
	constants := map[string]string{
		"EventNodeStart":      EventNodeStart,
		"EventNodeEnd":        EventNodeEnd,
		"EventEdgeTransition": EventEdgeTransition,
		"EventToolCall":       EventToolCall,
		"EventToolResult":     EventToolResult,
		"EventModelCall":      EventModelCall,
		"EventModelResponse":  EventModelResponse,
		"EventCheckpoint":     EventCheckpoint,
		"EventInterrupt":      EventInterrupt,
		"EventCompleted":      EventCompleted,
		"EventError":          EventError,
	}

	seen := make(map[string]string)
	for name, val := range constants {
		if val == "" {
			t.Errorf("%s is empty", name)
		}
		if prev, ok := seen[val]; ok {
			t.Errorf("%s and %s have the same value %q", name, prev, val)
		}
		seen[val] = name
	}
}

func TestBroker_PublishAfterUnsubscribe(t *testing.T) {
	b := NewBroker()
	b.Subscribe("temp")
	b.Unsubscribe("temp")

	// Should not panic
	b.Publish(Event{Type: "after-unsub"})
}

func TestBroker_ResubscribeWithSameID(t *testing.T) {
	b := NewBroker()
	ch1 := b.Subscribe("reuse-id")
	b.Unsubscribe("reuse-id")

	// ch1 should be closed
	_, ok := <-ch1
	if ok {
		t.Error("old channel should be closed")
	}

	ch2 := b.Subscribe("reuse-id")
	b.Publish(Event{Type: "after-resub"})

	select {
	case evt := <-ch2:
		if evt.Type != "after-resub" {
			t.Errorf("Type = %q, want %q", evt.Type, "after-resub")
		}
	case <-time.After(time.Second):
		t.Error("timed out waiting for event on resubscribed channel")
	}

	b.Unsubscribe("reuse-id")
}
