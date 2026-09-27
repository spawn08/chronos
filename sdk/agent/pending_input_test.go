package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

// scriptedInput hands out one batch per drain; drains records how often the
// loop asked.
type scriptedInput struct {
	mu      sync.Mutex
	batches [][]string
	drains  int
}

func (s *scriptedInput) DrainPendingInput(context.Context) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drains++
	if len(s.batches) == 0 {
		return nil
	}
	batch := s.batches[0]
	s.batches = s.batches[1:]
	return batch
}

func (s *scriptedInput) remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.batches)
}

// contentProvider records every request's messages.
type contentProvider struct {
	recordingProvider
	requests [][]model.Message
}

func (p *contentProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.mu.Lock()
	p.requests = append(p.requests, append([]model.Message(nil), req.Messages...))
	p.mu.Unlock()
	return p.recordingProvider.Chat(ctx, req)
}

func TestPendingInputJoinsRunningTaskAfterToolRound(t *testing.T) {
	calls := 0
	prov := &contentProvider{recordingProvider: recordingProvider{replies: toolRounds(2, "done")}}
	a := countingToolAgent(t, prov, &calls)
	input := &scriptedInput{batches: [][]string{{"also handle nil", "  ", "and add a test"}}}
	ctx := WithPendingInput(context.Background(), a.ID, input)

	resp, err := a.Chat(ctx, "fix the parser")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "done" || calls != 2 {
		t.Fatalf("content = %q, tool calls = %d", resp.Content, calls)
	}
	second := prov.requests[1]
	last := second[len(second)-1]
	if last.Role != model.RoleUser || last.Content != "also handle nil\n\nand add a test" {
		t.Fatalf("second request tail = %+v, want the pending input after the tool result", last)
	}
	if prev := second[len(second)-2]; prev.Role != model.RoleTool {
		t.Fatalf("message before pending input = %+v, want the round's tool result", prev)
	}
	third := prov.requests[2]
	if got := third[len(third)-1]; got.Role != model.RoleTool {
		t.Fatalf("third request tail = %+v, want no repeated input", got)
	}
	if input.drains != 2 {
		t.Fatalf("drains = %d, want one per completed round", input.drains)
	}
}

func TestPendingInputStaysPendingWhenControllerStops(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(3, "done")}, &calls)
	input := &scriptedInput{batches: [][]string{{"late context"}}}
	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{stopAt: 1})
	ctx = WithPendingInput(ctx, a.ID, input)

	resp, err := a.Chat(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != model.StopReasonPaused || input.drains != 0 || input.remaining() != 1 {
		t.Fatalf("stop = %q, drains = %d, remaining = %d", resp.StopReason, input.drains, input.remaining())
	}
}

func TestPendingInputIsBoundToOneAgent(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(1, "done")}, &calls)
	input := &scriptedInput{batches: [][]string{{"for the parent"}}}
	ctx := WithPendingInput(context.Background(), "parent", input)

	if _, err := a.Chat(ctx, "go"); err != nil {
		t.Fatal(err)
	}
	if input.drains != 0 {
		t.Fatalf("drains = %d, want a subagent to leave the caller's input alone", input.drains)
	}
}

func TestSessionPersistsPendingInputInOrder(t *testing.T) {
	for _, persistRounds := range []bool{true, false} {
		calls := 0
		prov := &recordingProvider{replies: toolRounds(1, "done")}
		a := countingToolAgent(t, prov, &calls)
		store := newTestStorage()
		a.Storage = store
		a.ContextCfg.PersistToolRounds = persistRounds
		ctx := WithPendingInput(context.Background(), a.ID, &scriptedInput{batches: [][]string{{"use v2 API"}}})

		if _, err := a.ChatWithSession(ctx, "s1", "task"); err != nil {
			t.Fatal(err)
		}
		want := "user,user,assistant"
		if persistRounds {
			want = "user,assistant,tool,user,assistant"
		}
		if got := strings.Join(sessionRoles(t, store, "s1"), ","); got != want {
			t.Fatalf("persist rounds %v: roles = %s, want %s", persistRounds, got, want)
		}
		events, _ := store.ListEvents(context.Background(), "s1", 0)
		msgs := chatSessionFromEvents(events).Messages
		if msgs[len(msgs)-2].Content != "use v2 API" {
			t.Fatalf("persisted input = %+v", msgs[len(msgs)-2])
		}
	}
}

func TestStreamSessionAppliesPendingInput(t *testing.T) {
	round := []*model.ChatResponse{{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "search", Arguments: `{}`}}, StopReason: model.StopReasonToolCall}}
	final := []*model.ChatResponse{{Role: model.RoleAssistant, Content: "done", StopReason: model.StopReasonEnd}}
	prov := &streamProvider{scripts: [][]*model.ChatResponse{round, final}}
	calls := 0
	a := countingToolAgent(t, prov, &calls)
	store := newTestStorage()
	a.Storage = store
	a.ContextCfg.PersistToolRounds = true
	ctx := WithPendingInput(context.Background(), a.ID, &scriptedInput{batches: [][]string{{"skip the docs"}}})

	ch, err := a.ChatStreamWithSession(ctx, "s1", "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectStream(t, ch); err != nil {
		t.Fatal(err)
	}
	follow := prov.requests[1].Messages
	if got := follow[len(follow)-1]; got.Role != model.RoleUser || got.Content != "skip the docs" {
		t.Fatalf("follow-up tail = %+v", got)
	}
	if got := strings.Join(sessionRoles(t, store, "s1"), ","); got != "user,assistant,tool,user,assistant" {
		t.Fatalf("persisted roles = %s", got)
	}
}
