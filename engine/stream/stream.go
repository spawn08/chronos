// Package stream provides event streaming for real-time observability.
package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Event is a server-sent event.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Broker manages SSE subscribers.
type Broker struct {
	mu      sync.RWMutex
	clients map[string]chan Event
}

// NewBroker creates a new event broker.
func NewBroker() *Broker {
	return &Broker{clients: make(map[string]chan Event)}
}

// Subscribe creates a new subscription channel.
func (b *Broker) Subscribe(id string) <-chan Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 64)
	b.clients[id] = ch
	return ch
}

// Unsubscribe removes a subscription.
func (b *Broker) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.clients[id]; ok {
		close(ch)
		delete(b.clients, id)
	}
}

// Publish sends an event to all subscribers.
func (b *Broker) Publish(evt Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.clients {
		select {
		case ch <- evt:
		default:
		}
	}
}

// SSEHandler returns an HTTP handler that streams events to the client.
func (b *Broker) SSEHandler(subscriptionID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := b.Subscribe(subscriptionID)
		defer b.Unsubscribe(subscriptionID)

		for {
			select {
			case evt, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(evt)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}
