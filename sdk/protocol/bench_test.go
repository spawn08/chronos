package protocol

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"testing"
)

// BenchmarkSendAndWait measures the synchronous request/reply round-trip through
// the bus, including correlation-id waiter registration and handler dispatch.
// It is self-pacing (one request in flight at a time) so it never trips
// back-pressure.
func BenchmarkSendAndWait(b *testing.B) {
	bus := NewBus()
	defer bus.Close()

	if err := bus.Register("client", "C", "", nil, nil); err != nil {
		b.Fatalf("register client: %v", err)
	}
	if err := bus.Register("server", "S", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{Type: TypeAnswer, Body: env.Body}, nil
	}); err != nil {
		b.Fatalf("register server: %v", err)
	}

	ctx := context.Background()
	body := json.RawMessage(`{"q":"ping"}`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := bus.SendAndWait(ctx, &Envelope{
			Type: TypeQuestion,
			From: "client",
			To:   "server",
			Body: body,
		}); err != nil {
			b.Fatalf("send and wait: %v", err)
		}
	}
}

// BenchmarkSendParallel measures the concurrent request/reply throughput of the
// bus with many senders sharing one server, exercising the handler pool and
// reply-routing map under contention.
func BenchmarkSendParallel(b *testing.B) {
	bus := NewBusWithConfig(BusConfig{InboxSize: 4096, MaxConcurrentHandlers: 1024})
	defer bus.Close()

	if err := bus.Register("server", "S", "", nil, func(_ context.Context, env *Envelope) (*Envelope, error) {
		return &Envelope{Type: TypeAnswer, Body: env.Body}, nil
	}); err != nil {
		b.Fatalf("register server: %v", err)
	}

	body := json.RawMessage(`{"q":"ping"}`)
	var seq atomic.Int64

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		// Each goroutine registers its own sender peer so replies route cleanly.
		id := "sender-" + strconv.FormatInt(seq.Add(1), 10)
		if err := bus.Register(id, "C", "", nil, nil); err != nil {
			b.Errorf("register sender: %v", err)
			return
		}
		for pb.Next() {
			if _, err := bus.SendAndWait(ctx, &Envelope{
				Type: TypeQuestion,
				From: id,
				To:   "server",
				Body: body,
			}); err != nil {
				b.Errorf("send and wait: %v", err)
				return
			}
		}
	})
}

// BenchmarkDirectChannel measures the low-latency point-to-point path that
// bypasses the central router.
func BenchmarkDirectChannel(b *testing.B) {
	dc := NewDirectChannel(1)
	defer dc.Close()
	env := &Envelope{Type: TypeStatus}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dc.AtoB <- env
		<-dc.AtoB
	}
}

// BenchmarkEnvelopePool measures the acquire/release round-trip of the envelope
// pool used by high-throughput senders.
func BenchmarkEnvelopePool(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := AcquireEnvelope()
		e.Type = TypeTaskRequest
		e.Subject = "work"
		ReleaseEnvelope(e)
	}
}
