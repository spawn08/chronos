package agent

import (
	"context"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
)

func TestToolInputProgress_PublishesStartAndThrottledSize(t *testing.T) {
	broker := stream.NewBroker()
	defer broker.Close()
	sub := broker.Subscribe("t")
	defer broker.Unsubscribe("t")

	now := time.Unix(0, 0)
	p := &toolInputProgress{agent: &Agent{ID: "a1", Broker: broker}, now: func() time.Time { return now }}
	ctx := context.Background()

	p.observe(ctx, []model.ToolCall{{ID: "call_1", Name: "file_write"}})             // start: published
	p.observe(ctx, []model.ToolCall{{Arguments: `{"path":"a.go",`}})                 // within interval: held
	now = now.Add(toolInputInterval)                                                 //
	p.observe(ctx, []model.ToolCall{{Arguments: `"content":"xyz"}`}})                // interval elapsed: published
	p.observe(ctx, []model.ToolCall{{ID: "call_2", Name: "shell", Arguments: "{}"}}) // next call: published

	type got struct {
		id, tool string
		bytes    int
	}
	var events []got
	deadline := time.After(time.Second)
	for len(events) < 3 {
		select {
		case e := <-sub:
			if e.Type != stream.EventToolInput {
				continue
			}
			d := e.Data.(map[string]any)
			if d["agent"] != "a1" {
				t.Fatalf("agent = %v", d["agent"])
			}
			events = append(events, got{d["id"].(string), d["tool"].(string), d["bytes"].(int)})
		case <-deadline:
			t.Fatalf("got %d tool_input events, want 3: %+v", len(events), events)
		}
	}
	want := []got{{"call_1", "file_write", 0}, {"call_1", "file_write", 31}, {"call_2", "shell", 2}}
	for i := range want {
		if events[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, events[i], want[i])
		}
	}
}
