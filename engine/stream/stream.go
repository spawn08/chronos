// Package stream provides event streaming for real-time observability.
package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Standard event types published during graph and agent execution.
const (
	EventNodeStart      = "node_start"
	EventNodeEnd        = "node_end"
	EventEdgeTransition = "edge_transition"
	EventToolCall       = "tool_call"
	EventToolResult     = "tool_result"
	EventModelCall      = "model_call"
	EventModelDelta     = "model_delta"
	EventModelResponse  = "model_response"
	EventCheckpoint     = "checkpoint"
	EventInterrupt      = "interrupt"
	EventCompleted      = "completed"
	EventError          = "error"
	EventPlanUpdate     = "plan_update"
	EventCustom         = "custom"
)

// Default Broker configuration values.
const (
	defaultMaxSubscribers = 1024
	defaultBufferSize     = 64
	defaultHeartbeat      = 15 * time.Second
)

// TopicQueryParams are the request query parameters, in priority order, that
// SSEHandler reads to determine the topic a client subscribes to.
var TopicQueryParams = []string{"topic", "session"}

// Event is a server-sent event.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Option configures a Broker.
type Option func(*Broker)

// WithMaxSubscribers bounds the number of concurrent subscribers. Subscriptions
// beyond the cap are rejected. A value <= 0 disables the cap.
func WithMaxSubscribers(n int) Option {
	return func(b *Broker) { b.maxSubs = n }
}

// WithBufferSize sets the per-subscriber channel buffer size. Events are dropped
// for a subscriber once its buffer fills, so slow consumers never block
// publishers.
func WithBufferSize(n int) Option {
	return func(b *Broker) {
		if n > 0 {
			b.bufferSize = n
		}
	}
}

// WithHeartbeat sets the interval at which SSEHandler emits keepalive comments.
// A value <= 0 disables heartbeats.
func WithHeartbeat(d time.Duration) Option {
	return func(b *Broker) { b.heartbeat = d }
}

// WithFanOut installs a custom FanOut, allowing event delivery to be
// externalized (e.g. Redis/NATS) for multi-replica deployments. When set, the
// max-subscriber and buffer-size options apply to the supplied FanOut only if
// it honors them; the Broker delegates delivery to it verbatim.
func WithFanOut(f FanOut) Option {
	return func(b *Broker) { b.fanout = f }
}

// Broker manages SSE subscribers. It routes events per topic (typically a
// session or tenant id) so a subscriber only receives events for its own topic,
// while a broadcast Publish still reaches every subscriber for backward
// compatibility. Delivery is delegated to a pluggable FanOut.
type Broker struct {
	fanout     FanOut
	maxSubs    int
	bufferSize int
	heartbeat  time.Duration

	// mu guards the legacy id map only.
	mu sync.Mutex
	// legacy maps a caller-supplied Subscribe id to the internal subscription
	// id, preserving the historic Subscribe/Unsubscribe(id) API.
	legacy map[string]string
}

// NewBroker creates a new event broker. Without options it uses an in-process
// FanOut with sane defaults (max 1024 subscribers, 64-event buffers, 15s
// heartbeat).
func NewBroker(opts ...Option) *Broker {
	b := &Broker{
		maxSubs:    defaultMaxSubscribers,
		bufferSize: defaultBufferSize,
		heartbeat:  defaultHeartbeat,
		legacy:     make(map[string]string),
	}
	for _, opt := range opts {
		opt(b)
	}
	if b.fanout == nil {
		b.fanout = newInProcessFanOut(b.maxSubs, b.bufferSize)
	}
	return b
}

// Subscribe creates a firehose subscription under the caller-supplied id and
// returns its channel. It exists for backward compatibility: with the empty
// topic the subscription receives every event — both broadcast Publish and any
// PublishTopic to a named session/tenant. (This is a deliberate change from the
// pre-routing behavior, where it saw only broadcasts; it is what the dashboard
// and monitor rely on.) Use SubscribeTopic for a session-scoped stream that is
// isolated from other sessions. Re-subscribing with an existing id replaces the
// previous subscription. If the subscriber cap is reached the returned channel
// is already closed.
func (b *Broker) Subscribe(id string) <-chan Event {
	b.mu.Lock()
	if old, ok := b.legacy[id]; ok {
		delete(b.legacy, id)
		b.mu.Unlock()
		b.fanout.Unsubscribe(old)
	} else {
		b.mu.Unlock()
	}

	sub, err := b.fanout.Subscribe("")
	if err != nil {
		ch := make(chan Event)
		close(ch)
		return ch
	}

	b.mu.Lock()
	b.legacy[id] = sub.ID
	b.mu.Unlock()
	return sub.C
}

// SubscribeTopic creates a subscription scoped to topic. Only events published
// to that topic (or broadcast events) are delivered. Unsubscribe with the
// returned Subscription.ID.
func (b *Broker) SubscribeTopic(topic string) (*Subscription, error) {
	sub, err := b.fanout.Subscribe(topic)
	if err != nil {
		return nil, fmt.Errorf("stream: subscribe topic %q: %w", topic, err)
	}
	return sub, nil
}

// Unsubscribe removes a subscription. It accepts either a caller-supplied
// Subscribe id or a Subscription.ID returned by SubscribeTopic.
func (b *Broker) Unsubscribe(id string) {
	b.mu.Lock()
	internal, ok := b.legacy[id]
	if ok {
		delete(b.legacy, id)
	}
	b.mu.Unlock()

	if ok {
		b.fanout.Unsubscribe(internal)
		return
	}
	b.fanout.Unsubscribe(id)
}

// Publish broadcasts an event to every subscriber, regardless of topic. Prefer
// PublishTopic to route to a single session/tenant and avoid cross-session
// leakage.
func (b *Broker) Publish(evt Event) {
	b.fanout.Publish("", evt)
}

// PublishTopic delivers an event only to subscribers of the given topic.
func (b *Broker) PublishTopic(topic string, evt Event) {
	b.fanout.Publish(topic, evt)
}

// Heartbeat returns the broker's configured SSE keepalive interval, so
// alternative SSE handlers (e.g. the AG-UI stream) can honor the same
// configuration instead of hardcoding their own. A value <= 0 means heartbeats
// are disabled.
func (b *Broker) Heartbeat() time.Duration {
	return b.heartbeat
}

// Close releases all subscriptions and underlying resources.
func (b *Broker) Close() error {
	if err := b.fanout.Close(); err != nil {
		return fmt.Errorf("stream: close broker: %w", err)
	}
	return nil
}

// topicFromRequest resolves the subscription topic for an SSE request, reading
// the query parameters in TopicQueryParams order and falling back to
// defaultTopic when none is present.
func topicFromRequest(r *http.Request, defaultTopic string) string {
	q := r.URL.Query()
	for _, p := range TopicQueryParams {
		if v := q.Get(p); v != "" {
			return v
		}
	}
	return defaultTopic
}

// SSEHandler returns an HTTP handler that streams events to the client over
// Server-Sent Events. The topic each client subscribes to is taken from the
// request query (see TopicQueryParams), falling back to defaultTopic. Each
// connection gets a unique subscriber id, so concurrent clients never clobber
// one another. Periodic keepalive comments (see WithHeartbeat) keep idle
// connections and intermediary proxies from timing out, and the subscription is
// released when the client disconnects (request context is canceled) or the
// subscriber cap is exceeded (HTTP 503).
func (b *Broker) SSEHandler(defaultTopic string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		sub, err := b.fanout.Subscribe(topicFromRequest(r, defaultTopic))
		if err != nil {
			http.Error(w, "too many subscribers", http.StatusServiceUnavailable)
			return
		}
		defer b.fanout.Unsubscribe(sub.ID)

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		var ticker *time.Ticker
		var tick <-chan time.Time
		if b.heartbeat > 0 {
			ticker = time.NewTicker(b.heartbeat)
			defer ticker.Stop()
			tick = ticker.C
		}

		for {
			select {
			case evt, ok := <-sub.C:
				if !ok {
					return
				}
				data, err := json.Marshal(evt)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
				flusher.Flush()
			case <-tick:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
