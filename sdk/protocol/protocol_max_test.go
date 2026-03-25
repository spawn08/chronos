package protocol

import (
	"context"
	"sync"
	"testing"
)

func TestBus_Close_Idempotent(t *testing.T) {
	b := NewBus()
	b.Close()
	b.Close()
	if err := b.Send(context.Background(), &Envelope{From: "a", To: "b", Body: []byte("{}")}); err == nil {
		t.Fatal("expected send error after close")
	}
}

func TestDirectChannelBetween_ConcurrentFirstCreate(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dc := b.DirectChannelBetween("x", "y", 2)
			if dc == nil {
				t.Error("nil direct channel")
			}
		}()
	}
	wg.Wait()
	dc := b.DirectChannelBetween("y", "x", 2)
	if dc == nil {
		t.Fatal("nil channel for reverse key order")
	}
}

func TestBroadcast_SendAfterClose(t *testing.T) {
	b := NewBus()
	b.Register("a", "A", "", nil, nil)
	b.Register("b", "B", "", nil, nil)
	b.Close()
	err := b.Send(context.Background(), &Envelope{Type: TypeBroadcast, From: "a", To: "*", Body: []byte("{}")})
	if err == nil {
		t.Fatal("expected error broadcasting on closed bus")
	}
}
