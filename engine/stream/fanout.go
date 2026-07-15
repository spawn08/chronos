package stream

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
)

// Subscription is a handle to an active fan-out subscription. ID is unique for
// the lifetime of the FanOut and is used to Unsubscribe; C delivers events for
// the subscription's Topic.
type Subscription struct {
	ID    string
	Topic string
	C     <-chan Event
}

// FanOut delivers published events to interested subscribers. The default
// implementation (newInProcessFanOut) keeps all state in memory. Multi-replica
// deployments can supply an external implementation (backed by Redis, NATS,
// etc.) via WithFanOut so events published on one replica reach subscribers on
// another. Implementations must be safe for concurrent use.
type FanOut interface {
	// Subscribe registers a new subscriber for topic and returns its handle.
	// It returns an error when the subscriber cap is reached.
	Subscribe(topic string) (*Subscription, error)
	// Unsubscribe removes the subscriber with the given id and releases its
	// resources. It is a no-op for an unknown id.
	Unsubscribe(id string)
	// Publish delivers evt to every subscriber of topic. An empty topic is a
	// broadcast that reaches every subscriber regardless of topic. Delivery is
	// best-effort: a subscriber whose buffer is full drops the event rather
	// than blocking publishers.
	Publish(topic string, evt Event)
	// Close releases all subscriptions and underlying resources.
	Close() error
}

// subscription is the internal, writable side of a Subscription.
type subscription struct {
	id    string
	topic string
	ch    chan Event
}

// inProcessFanOut is the default in-memory FanOut implementation.
type inProcessFanOut struct {
	mu         sync.RWMutex
	subs       map[string]*subscription
	maxSubs    int
	bufferSize int
	counter    atomic.Uint64
}

// newInProcessFanOut creates an in-memory fan-out bounded to maxSubs concurrent
// subscribers, each with a buffered channel of bufferSize events.
func newInProcessFanOut(maxSubs, bufferSize int) *inProcessFanOut {
	return &inProcessFanOut{
		subs:       make(map[string]*subscription),
		maxSubs:    maxSubs,
		bufferSize: bufferSize,
	}
}

// Subscribe implements FanOut.
func (f *inProcessFanOut) Subscribe(topic string) (*Subscription, error) {
	id, err := f.nextID()
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.maxSubs > 0 && len(f.subs) >= f.maxSubs {
		return nil, fmt.Errorf("stream: subscriber cap reached (%d)", f.maxSubs)
	}
	ch := make(chan Event, f.bufferSize)
	f.subs[id] = &subscription{id: id, topic: topic, ch: ch}
	return &Subscription{ID: id, Topic: topic, C: ch}, nil
}

// Unsubscribe implements FanOut.
func (f *inProcessFanOut) Unsubscribe(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.subs[id]; ok {
		close(s.ch)
		delete(f.subs, id)
	}
}

// Publish implements FanOut.
func (f *inProcessFanOut) Publish(topic string, evt Event) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, s := range f.subs {
		if topic != "" && s.topic != topic {
			continue
		}
		select {
		case s.ch <- evt:
		default:
		}
	}
}

// Close implements FanOut.
func (f *inProcessFanOut) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.subs {
		close(s.ch)
		delete(f.subs, id)
	}
	return nil
}

// nextID returns a process-unique subscriber id. A monotonic counter guarantees
// uniqueness even when two subscriptions are created within the same instant;
// the random prefix keeps ids unpredictable.
func (f *inProcessFanOut) nextID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("stream: generate subscriber id: %w", err)
	}
	n := f.counter.Add(1)
	return fmt.Sprintf("%s-%d", hex.EncodeToString(buf[:]), n), nil
}
