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

	// Token budget: enforceContextBudget guarantees the assembled request fits the
	// configured limit (the protected pins here are small), so this is a true
	// invariant, not a fixture coincidence.
	counter := model.NewTokenCounter(prov.Model())
	if got := counter.CountTokens(main.Messages); got > a.resolveContextLimit() {
		t.Errorf("compacted request %d tokens exceeds limit %d", got, a.resolveContextLimit())
	}
}

func TestEnforceContextBudget(t *testing.T) {
	sys := model.Message{Role: model.RoleSystem, Content: "PIN"}
	big := func(id string) model.Message {
		return model.Message{Role: model.RoleUser, Content: id + ": " + strings.Repeat("token ", 200)}
	}
	counter := model.NewEstimatingCounter()

	tests := []struct {
		name            string
		messages        []model.Message
		protectedPrefix int
		limit           int
		wantFits        bool // final count <= limit
		wantMinLen      int  // at least this many messages remain
	}{
		{
			name:            "under limit unchanged",
			messages:        []model.Message{sys, {Role: model.RoleUser, Content: "hi"}},
			protectedPrefix: 1,
			limit:           100000,
			wantFits:        true,
			wantMinLen:      2,
		},
		{
			name:            "zero limit is a no-op",
			messages:        []model.Message{sys, big("a"), big("b")},
			protectedPrefix: 1,
			limit:           0,
			wantMinLen:      3,
		},
		{
			name:            "trims oldest conversation turns to fit",
			messages:        []model.Message{sys, big("oldest"), big("mid"), big("newest")},
			protectedPrefix: 1,
			limit:           counter.CountTokens([]model.Message{sys, big("newest")}) + 5,
			wantFits:        true,
			wantMinLen:      2, // prefix + at least the last message
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy so we don't mutate the shared fixture across sub-tests.
			in := append([]model.Message(nil), tt.messages...)
			out := enforceContextBudget(counter, in, tt.protectedPrefix, tt.limit)

			if len(out) < tt.wantMinLen {
				t.Fatalf("len(out) = %d, want >= %d", len(out), tt.wantMinLen)
			}
			if tt.wantFits && counter.CountTokens(out) > tt.limit {
				t.Errorf("output %d tokens exceeds limit %d", counter.CountTokens(out), tt.limit)
			}
			// The protected prefix is always retained verbatim.
			for i := 0; i < tt.protectedPrefix && i < len(out); i++ {
				if out[i].Content != tt.messages[i].Content {
					t.Errorf("protected prefix[%d] changed: %q", i, out[i].Content)
				}
			}
		})
	}
}

// TestEnforceContextBudget_DropsOrphanedToolResult verifies that trimming an
// assistant turn carrying tool calls also drops the tool results that followed
// it, so the history never begins with an orphaned tool message.
func TestEnforceContextBudget_DropsOrphanedToolResult(t *testing.T) {
	sys := model.Message{Role: model.RoleSystem, Content: "PIN"}
	assistant := model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "c1", Name: "t"}}, Content: strings.Repeat("x ", 200)}
	toolResult := model.Message{Role: model.RoleTool, ToolCallID: "c1", Content: strings.Repeat("y ", 200)}
	final := model.Message{Role: model.RoleUser, Content: "latest question"}
	counter := model.NewEstimatingCounter()

	in := []model.Message{sys, assistant, toolResult, final}
	limit := counter.CountTokens([]model.Message{sys, final}) + 5
	out := enforceContextBudget(counter, in, 1, limit)

	if len(out) < 2 {
		t.Fatalf("expected prefix + last message, got %d", len(out))
	}
	if out[1].Role == model.RoleTool {
		t.Errorf("history begins with orphaned tool result: %+v", out[1])
	}
}

func BenchmarkContextCompaction(b *testing.B) {
	counter := model.NewTokenCounter("gpt-4o")
	base := []model.Message{{Role: model.RoleSystem, Content: "system + pins"}}
	msgs := make([]model.Message, 0, 121)
	msgs = append(msgs, base...)
	for i := 0; i < 120; i++ {
		msgs = append(msgs, model.Message{Role: model.RoleUser, Content: strings.Repeat("token ", 50)})
	}
	limit := 2000

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		in := append([]model.Message(nil), msgs...)
		_ = enforceContextBudget(counter, in, len(base), limit)
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
