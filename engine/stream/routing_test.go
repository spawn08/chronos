package stream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// drain reads all currently-buffered events from ch without blocking.
func drain(ch <-chan Event) []Event {
	var out []Event
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

// TestBroker_PublishTopic_Isolation proves an event published to one topic is
// not delivered to a subscriber on a different topic (no cross-session leak).
func TestBroker_PublishTopic_Isolation(t *testing.T) {
	tests := []struct {
		name        string
		publishTo   string
		wantA       bool
		wantB       bool
		wantGlobalC bool
	}{
		{name: "topic A only", publishTo: "session-A", wantA: true, wantB: false, wantGlobalC: false},
		{name: "topic B only", publishTo: "session-B", wantA: false, wantB: true, wantGlobalC: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker()
			defer b.Close()

			subA, err := b.SubscribeTopic("session-A")
			if err != nil {
				t.Fatalf("subscribe A: %v", err)
			}
			subB, err := b.SubscribeTopic("session-B")
			if err != nil {
				t.Fatalf("subscribe B: %v", err)
			}
			// A global (broadcast) subscriber via the legacy API.
			chC := b.Subscribe("legacy-global")

			b.PublishTopic(tc.publishTo, Event{Type: EventCustom, Data: "x"})
			time.Sleep(20 * time.Millisecond)

			if got := len(drain(subA.C)) > 0; got != tc.wantA {
				t.Errorf("subscriber A received=%v, want %v", got, tc.wantA)
			}
			if got := len(drain(subB.C)) > 0; got != tc.wantB {
				t.Errorf("subscriber B received=%v, want %v", got, tc.wantB)
			}
			if got := len(drain(chC)) > 0; got != tc.wantGlobalC {
				t.Errorf("global subscriber received=%v, want %v (topic publish must not reach global)", got, tc.wantGlobalC)
			}
		})
	}
}

// TestBroker_Broadcast_ReachesAll confirms the legacy Publish still reaches
// every subscriber regardless of topic (backward compatibility).
func TestBroker_Broadcast_ReachesAll(t *testing.T) {
	b := NewBroker()
	defer b.Close()

	subA, _ := b.SubscribeTopic("A")
	chGlobal := b.Subscribe("g")

	b.Publish(Event{Type: EventCompleted, Data: "done"})
	time.Sleep(20 * time.Millisecond)

	if len(drain(subA.C)) == 0 {
		t.Error("topic subscriber should receive broadcast events")
	}
	if len(drain(chGlobal)) == 0 {
		t.Error("global subscriber should receive broadcast events")
	}
}

// TestBroker_UniqueSubscriberIDs proves concurrently-created subscriptions get
// distinct ids that never collide.
func TestBroker_UniqueSubscriberIDs(t *testing.T) {
	b := NewBroker(WithMaxSubscribers(0)) // uncapped
	defer b.Close()

	const n = 500
	var (
		mu   sync.Mutex
		seen = make(map[string]struct{}, n)
		wg   sync.WaitGroup
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			sub, err := b.SubscribeTopic("t")
			if err != nil {
				t.Errorf("subscribe: %v", err)
				return
			}
			mu.Lock()
			if _, dup := seen[sub.ID]; dup {
				t.Errorf("duplicate subscriber id: %s", sub.ID)
			}
			seen[sub.ID] = struct{}{}
			mu.Unlock()
		}()
	}
	wg.Wait()

	mu.Lock()
	got := len(seen)
	mu.Unlock()
	if got != n {
		t.Errorf("unique ids = %d, want %d", got, n)
	}
}

// TestBroker_SubscriberCap enforces the configured maximum.
func TestBroker_SubscriberCap(t *testing.T) {
	tests := []struct {
		name string
		cap  int
	}{
		{name: "cap 1", cap: 1},
		{name: "cap 3", cap: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker(WithMaxSubscribers(tc.cap))
			defer b.Close()

			for i := 0; i < tc.cap; i++ {
				if _, err := b.SubscribeTopic("t"); err != nil {
					t.Fatalf("subscription %d within cap failed: %v", i, err)
				}
			}
			if _, err := b.SubscribeTopic("t"); err == nil {
				t.Fatalf("subscription beyond cap %d should be rejected", tc.cap)
			}
		})
	}
}

// TestBroker_SubscriberCap_LegacyClosedChannel verifies the legacy Subscribe API
// sheds beyond the cap by returning an already-closed channel.
func TestBroker_SubscriberCap_LegacyClosedChannel(t *testing.T) {
	b := NewBroker(WithMaxSubscribers(1))
	defer b.Close()

	b.Subscribe("first")
	ch := b.Subscribe("second") // over cap

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("over-cap legacy channel should be closed, got a value")
		}
	case <-time.After(time.Second):
		t.Error("over-cap legacy channel should be closed, blocked instead")
	}
}

// TestSSEHandler_TopicFromQuery routes an SSE client to the topic named in the
// request query.
func TestSSEHandler_TopicFromQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		topic string
	}{
		{name: "topic param", query: "?topic=session-42", topic: "session-42"},
		{name: "session param", query: "?session=tenant-9", topic: "tenant-9"},
		{name: "default fallback", query: "", topic: "dashboard"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBroker(WithHeartbeat(0))
			defer b.Close()
			handler := b.SSEHandler("dashboard")

			req := httptest.NewRequest(http.MethodGet, "/events"+tc.query, http.NoBody)
			ctx, cancel := context.WithCancel(req.Context())
			req = req.WithContext(ctx)
			w := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer close(done)
				handler(w, req)
			}()

			time.Sleep(50 * time.Millisecond)
			b.PublishTopic(tc.topic, Event{Type: EventModelCall, Data: "hi"})
			time.Sleep(50 * time.Millisecond)
			cancel()
			<-done

			if !strings.Contains(w.Body.String(), "event: model_call") {
				t.Errorf("client on topic %q missing routed event; got: %q", tc.topic, w.Body.String())
			}
		})
	}
}

// TestSSEHandler_TopicIsolation ensures an SSE client on topic A does not see an
// event published to topic B.
func TestSSEHandler_TopicIsolation(t *testing.T) {
	b := NewBroker(WithHeartbeat(0))
	defer b.Close()
	handler := b.SSEHandler("dashboard")

	req := httptest.NewRequest(http.MethodGet, "/events?topic=A", http.NoBody)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(w, req)
	}()

	time.Sleep(50 * time.Millisecond)
	b.PublishTopic("B", Event{Type: EventModelCall, Data: "for-B"})
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if strings.Contains(w.Body.String(), "model_call") {
		t.Errorf("client on topic A leaked topic-B event; got: %q", w.Body.String())
	}
}

// TestSSEHandler_Heartbeat verifies keepalive comments are emitted on idle
// connections.
func TestSSEHandler_Heartbeat(t *testing.T) {
	b := NewBroker(WithHeartbeat(20 * time.Millisecond))
	defer b.Close()
	handler := b.SSEHandler("dashboard")

	req := httptest.NewRequest(http.MethodGet, "/events", http.NoBody)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(w, req)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(w.Body.String(), ": keepalive") {
		t.Errorf("expected keepalive comments on idle connection; got: %q", w.Body.String())
	}
}

// TestSSEHandler_DisconnectFreesSubscription proves a disconnecting client frees
// its subscription and the handler goroutine exits.
func TestSSEHandler_DisconnectFreesSubscription(t *testing.T) {
	b := NewBroker(WithHeartbeat(0))
	defer b.Close()

	fo, ok := b.fanout.(*inProcessFanOut)
	if !ok {
		t.Fatal("expected in-process fan-out")
	}
	subsLen := func() int {
		fo.mu.RLock()
		defer fo.mu.RUnlock()
		return len(fo.subs)
	}

	before := runtime.NumGoroutine()
	handler := b.SSEHandler("dashboard")

	req := httptest.NewRequest(http.MethodGet, "/events?topic=s1", http.NoBody)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		handler(w, req)
	}()

	// Wait for the subscription to register.
	deadline := time.Now().Add(time.Second)
	for subsLen() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if subsLen() != 1 {
		t.Fatalf("expected 1 active subscription, got %d", subsLen())
	}

	cancel() // client disconnects
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler goroutine did not exit after client disconnect")
	}

	if got := subsLen(); got != 0 {
		t.Errorf("subscription not freed after disconnect: %d remaining", got)
	}

	// Goroutine count should return to baseline (allow scheduler slack).
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+2 {
		t.Errorf("goroutine leak: before=%d after=%d", before, after)
	}
}

// fakeFanOut is a minimal external-style FanOut used to prove pluggability.
type fakeFanOut struct {
	mu        sync.Mutex
	published []struct {
		topic string
		evt   Event
	}
	subs   map[string]chan Event
	closed bool
	seq    int
}

func newFakeFanOut() *fakeFanOut {
	return &fakeFanOut{subs: make(map[string]chan Event)}
}

func (f *fakeFanOut) Subscribe(topic string) (*Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := topic + "-fake-" + string(rune('0'+f.seq))
	ch := make(chan Event, 8)
	f.subs[id] = ch
	return &Subscription{ID: id, Topic: topic, C: ch}, nil
}

func (f *fakeFanOut) Unsubscribe(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ch, ok := f.subs[id]; ok {
		close(ch)
		delete(f.subs, id)
	}
}

func (f *fakeFanOut) Publish(topic string, evt Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, struct {
		topic string
		evt   Event
	}{topic, evt})
}

func (f *fakeFanOut) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

// TestBroker_PluggableFanOut proves the Broker delegates to an injected FanOut.
func TestBroker_PluggableFanOut(t *testing.T) {
	fake := newFakeFanOut()
	b := NewBroker(WithFanOut(fake))

	if _, err := b.SubscribeTopic("t1"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	b.PublishTopic("t1", Event{Type: EventCustom, Data: "hi"})
	b.Publish(Event{Type: EventCompleted})

	fake.mu.Lock()
	got := len(fake.published)
	firstTopic := fake.published[0].topic
	secondTopic := fake.published[1].topic
	fake.mu.Unlock()

	if got != 2 {
		t.Fatalf("expected 2 publishes delegated to fan-out, got %d", got)
	}
	if firstTopic != "t1" {
		t.Errorf("PublishTopic delegated topic = %q, want t1", firstTopic)
	}
	if secondTopic != "" {
		t.Errorf("broadcast Publish delegated topic = %q, want empty", secondTopic)
	}

	if err := b.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	fake.mu.Lock()
	closed := fake.closed
	fake.mu.Unlock()
	if !closed {
		t.Error("Close should propagate to fan-out")
	}
}

// TestFanOut_ConcurrentPublishSubscribe exercises the in-process fan-out under
// concurrent load (run with -race).
func TestFanOut_ConcurrentPublishSubscribe(t *testing.T) {
	b := NewBroker(WithMaxSubscribers(0))
	defer b.Close()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publishers.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.PublishTopic("t", Event{Type: EventCustom, Data: "x"})
					b.Publish(Event{Type: EventCustom, Data: "y"})
				}
			}
		}()
	}

	// Subscribers churning.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sub, err := b.SubscribeTopic("t")
				if err != nil {
					continue
				}
				drain(sub.C)
				b.Unsubscribe(sub.ID)
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()
}
