package agent

import (
	"context"
	"testing"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

// collectBrokerEvents drains a broker subscription until done fires, returning
// the events by type.
func collectBrokerEvents(sub <-chan stream.Event, done <-chan struct{}) []stream.Event {
	var out []stream.Event
	for {
		select {
		case e, ok := <-sub:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-done:
			// Drain anything already buffered, then stop.
			for {
				select {
				case e := <-sub:
					out = append(out, e)
				default:
					return out
				}
			}
		}
	}
}

// The blocking Chat path must publish the post-tool FINAL answer as a
// model_response carrying content, not just the pre-tool response — otherwise a
// tool-using turn drops its answer text from the event stream (regression guard
// for the AG-UI blocking-path gap).
func TestChat_PublishesFinalAnswerAfterTools(t *testing.T) {
	p := &recordingProvider{
		replies: []*model.ChatResponse{
			toolCallResp("t1", "search"),                                 // round 1: call a tool
			{StopReason: model.StopReasonEnd, Content: "the final text"}, // round 2: answer
		},
	}
	broker := stream.NewBroker()
	defer broker.Close()

	a, _ := New("a1", "T").WithModel(p).WithBroker(broker).Build()
	a.Tools.Register(&tool.Definition{
		Name: "search", Permission: tool.PermAllow, Parameters: map[string]any{"type": "object"},
		Handler: func(context.Context, map[string]any) (any, error) { return "result", nil },
	})

	sub := broker.Subscribe("t")
	defer broker.Unsubscribe("t")

	if _, err := a.Chat(context.Background(), "q"); err != nil {
		t.Fatalf("chat: %v", err)
	}

	done := make(chan struct{})
	go func() { time.Sleep(100 * time.Millisecond); close(done) }()
	events := collectBrokerEvents(sub, done)

	var finalContent string
	sawToolCall := false
	for _, e := range events {
		data, _ := e.Data.(map[string]any)
		switch e.Type {
		case stream.EventToolCall:
			sawToolCall = true
		case stream.EventModelResponse:
			if c, _ := data["content"].(string); c != "" {
				finalContent = c
			}
		}
	}
	if !sawToolCall {
		t.Error("expected a tool_call event")
	}
	if finalContent != "the final text" {
		t.Errorf("final model_response content = %q, want %q (post-tool answer must be published)", finalContent, "the final text")
	}
}

// Agent events published while a session is in context are routed to that
// session's topic, so a per-session subscriber is isolated from other sessions'
// events (and a different session sees none of them).
func TestChat_PublishesToSessionTopic(t *testing.T) {
	p := &recordingProvider{replies: []*model.ChatResponse{{StopReason: model.StopReasonEnd, Content: "hi"}}}
	broker := stream.NewBroker()
	defer broker.Close()
	a, _ := New("a1", "T").WithModel(p).WithBroker(broker).Build()

	mine, _ := broker.SubscribeTopic("sess-A")
	other, _ := broker.SubscribeTopic("sess-B")
	defer broker.Unsubscribe(mine.ID)
	defer broker.Unsubscribe(other.ID)

	ctx := storage.WithSession(context.Background(), "sess-A")
	if _, err := a.Chat(ctx, "q"); err != nil {
		t.Fatalf("chat: %v", err)
	}

	// sess-A receives the model_response; sess-B receives nothing.
	select {
	case e := <-mine.C:
		if e.Type != stream.EventModelCall && e.Type != stream.EventModelResponse {
			t.Errorf("unexpected event on session topic: %s", e.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("session subscriber received no event")
	}
	select {
	case e := <-other.C:
		t.Errorf("other session leaked an event: %s", e.Type)
	case <-time.After(150 * time.Millisecond):
		// Correct: no cross-session leak.
	}
}
