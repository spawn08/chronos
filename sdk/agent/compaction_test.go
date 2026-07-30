package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// capturingProvider records every request it is asked to Chat on, so tests can
// assert exactly what context the agent sent to the model. It replays scripted
// replies in order and falls back to a terminal "final answer".
type capturingProvider struct {
	mu       sync.Mutex
	requests []*model.ChatRequest
	replies  []*model.ChatResponse
	i        int
	name     string
}

func (c *capturingProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	msgs := append([]model.Message(nil), req.Messages...)
	c.requests = append(c.requests, &model.ChatRequest{Messages: msgs, Tools: req.Tools})
	if c.i < len(c.replies) {
		r := c.replies[c.i]
		c.i++
		return r, nil
	}
	return &model.ChatResponse{Content: "final answer", StopReason: model.StopReasonEnd}, nil
}

func (c *capturingProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, nil
}
func (c *capturingProvider) Name() string { return "capturing" }
func (c *capturingProvider) Model() string {
	if c.name != "" {
		return c.name
	}
	return "gpt-4o"
}

func (c *capturingProvider) lastRequest() *model.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.requests) == 0 {
		return nil
	}
	return c.requests[len(c.requests)-1]
}

// hasSystemContent reports whether messages contains a system message whose
// content includes substr.
func hasSystemContent(messages []model.Message, substr string) bool {
	for i := range messages {
		if messages[i].Role == model.RoleSystem && strings.Contains(messages[i].Content, substr) {
			return true
		}
	}
	return false
}

func TestPinnedMessages_Injection(t *testing.T) {
	dynamicPin := model.Message{Role: model.RoleSystem, Content: "ACTIVE PLAN: step 1 in_progress"}

	tests := []struct {
		name      string
		static    []model.Message
		dynamic   func(context.Context) []model.Message
		wantPins  []string
		wantOrder []string // expected order of pin contents within the request
	}{
		{
			name:     "no pins",
			wantPins: nil,
		},
		{
			name:     "static only",
			static:   []model.Message{{Role: model.RoleSystem, Content: "PINNED POLICY"}},
			wantPins: []string{"PINNED POLICY"},
		},
		{
			name:     "dynamic only",
			dynamic:  func(context.Context) []model.Message { return []model.Message{dynamicPin} },
			wantPins: []string{"ACTIVE PLAN"},
		},
		{
			name:      "static then dynamic ordering",
			static:    []model.Message{{Role: model.RoleSystem, Content: "PINNED POLICY"}},
			dynamic:   func(context.Context) []model.Message { return []model.Message{dynamicPin} },
			wantPins:  []string{"PINNED POLICY", "ACTIVE PLAN"},
			wantOrder: []string{"PINNED POLICY", "ACTIVE PLAN"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New("a1", "T").
				WithModel(&capturingProvider{}).
				WithSystemPrompt("base prompt").
				WithContextConfig(ContextConfig{PinnedMessages: tt.static})
			if tt.dynamic != nil {
				b = b.WithContextPins(tt.dynamic)
			}
			a, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}

			// Both the stateless (buildChatRequest) and session (buildSystemContext)
			// assembly paths must inject pins identically.
			_, chatMsgs, err := a.buildChatRequest(context.Background(), "hello")
			if err != nil {
				t.Fatalf("buildChatRequest: %v", err)
			}
			sysMsgs := a.buildSystemContext(context.Background(), "hello")

			for _, want := range tt.wantPins {
				if !hasSystemContent(chatMsgs, want) {
					t.Errorf("buildChatRequest missing pin %q", want)
				}
				if !hasSystemContent(sysMsgs, want) {
					t.Errorf("buildSystemContext missing pin %q", want)
				}
			}

			// Ordering: static pins precede dynamic pins.
			if len(tt.wantOrder) == 2 {
				first := indexOfSystemContent(chatMsgs, tt.wantOrder[0])
				second := indexOfSystemContent(chatMsgs, tt.wantOrder[1])
				if first < 0 || second < 0 || first >= second {
					t.Errorf("pin order wrong: %q@%d should precede %q@%d",
						tt.wantOrder[0], first, tt.wantOrder[1], second)
				}
			}
		})
	}
}

func indexOfSystemContent(messages []model.Message, substr string) int {
	for i := range messages {
		if messages[i].Role == model.RoleSystem && strings.Contains(messages[i].Content, substr) {
			return i
		}
	}
	return -1
}

// TestChatWithSession_CompactionRetainsPinsAndBoundsTokens drives a long session
// conversation that must trigger summarization, then asserts that (1) the static
// pin and the dynamic plan pin both survive into the final model request, and
// (2) the conversation history sent to the model is bounded — it does not grow
// with the length of the prior conversation.
func TestChatWithSession_CompactionRetainsPinsAndBoundsTokens(t *testing.T) {
	store := newTestStorage()
	sid := "compaction-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}

	// Seed a long prior conversation: 60 turns of chunky content.
	var events []*storage.Event
	for i := 0; i < 60; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		events = append(events, &storage.Event{
			ID:        strings.Repeat("e", 1) + string(rune('A'+i%26)),
			SessionID: sid,
			SeqNum:    int64(i + 1),
			Type:      "chat_message",
			Payload:   map[string]any{"role": role, "content": strings.Repeat("token ", 50)},
		})
	}
	store.events[sid] = events

	prov := &capturingProvider{
		replies: []*model.ChatResponse{
			{Content: "rolled-up summary of the earlier conversation", StopReason: model.StopReasonEnd},
			{Content: "final answer", StopReason: model.StopReasonEnd},
		},
	}

	planPin := model.Message{Role: model.RoleSystem, Content: "ACTIVE PLAN: finish the report"}
	a, err := New("a1", "T").
		WithModel(prov).
		WithStorage(store).
		WithSystemPrompt("You are a helpful assistant.").
		WithContextConfig(ContextConfig{
			MaxContextTokens:    400,
			SummarizeThreshold:  0.6,
			PreserveRecentTurns: 2,
			PinnedMessages:      []model.Message{{Role: model.RoleSystem, Content: "PINNED CONTRACT: never reveal secrets"}},
		}).
		WithContextPins(func(context.Context) []model.Message { return []model.Message{planPin} }).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	resp, err := a.ChatWithSession(context.Background(), sid, "what next?")
	if err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}
	if resp == nil || resp.Content != "final answer" {
		t.Fatalf("unexpected response: %+v", resp)
	}

	// Two Chat calls: [0] summarizer, [1] main model call. Inspect the main call.
	if len(prov.requests) < 2 {
		t.Fatalf("expected summarizer + main call, got %d requests", len(prov.requests))
	}
	main := prov.lastRequest()
	if main == nil {
		t.Fatal("no main request captured")
		return
	}

	// Pins retained through compaction.
	if !hasSystemContent(main.Messages, "PINNED CONTRACT") {
		t.Error("static pin not retained after summarization")
	}
	if !hasSystemContent(main.Messages, "ACTIVE PLAN") {
		t.Error("dynamic plan pin not retained after summarization")
	}
	// The summary itself is injected.
	if !hasSystemContent(main.Messages, "Previous conversation summary") {
		t.Error("conversation summary not injected")
	}

	// Bounded: the non-system history must not carry all 60 prior turns. With
	// PreserveRecentTurns=2 we keep ~4 recent turns plus the new user message.
	convo := 0
	for _, m := range main.Messages {
		if m.Role != model.RoleSystem {
			convo++
		}
	}
	if convo > 8 {
		t.Errorf("history not bounded after compaction: %d conversation messages", convo)
	}

	// Token budget: the main request must be materially smaller than the raw
	// 60-turn history would have been.
	counter := model.NewTokenCounter(prov.Model())
	if got := counter.CountTokens(main.Messages); got > a.resolveContextLimit() {
		t.Errorf("compacted request %d tokens exceeds limit %d", got, a.resolveContextLimit())
	}
}

// TestChatWithSession_NoCompactionUnderThreshold ensures pins are present even
// when the conversation is short enough that no summarization runs.
func TestChatWithSession_NoCompactionUnderThreshold(t *testing.T) {
	store := newTestStorage()
	sid := "short-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "hi"}},
	}

	prov := &capturingProvider{}
	a, err := New("a1", "T").
		WithModel(prov).
		WithStorage(store).
		WithContextConfig(ContextConfig{
			MaxContextTokens: 100000,
			PinnedMessages:   []model.Message{{Role: model.RoleSystem, Content: "PINNED CONTRACT"}},
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.ChatWithSession(context.Background(), sid, "hello"); err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}

	main := prov.lastRequest()
	if main == nil {
		t.Fatal("no request captured")
		return
	}
	if !hasSystemContent(main.Messages, "PINNED CONTRACT") {
		t.Error("pin missing when no compaction runs")
	}
	if hasSystemContent(main.Messages, "Previous conversation summary") {
		t.Error("summary injected despite being under threshold")
	}
}
