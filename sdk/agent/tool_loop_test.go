package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
)

// stopAfterController stops the loop after stopAt rounds (0 = never) and
// records every round it sees.
type stopAfterController struct {
	mu     sync.Mutex
	stopAt int
	rounds []ToolRound
}

func (c *stopAfterController) AfterToolRound(_ context.Context, round ToolRound) (ToolLoopAction, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rounds = append(c.rounds, round)
	if c.stopAt > 0 && len(c.rounds) >= c.stopAt {
		return ToolLoopAction{Stop: true, Message: "paused: no progress"}, nil
	}
	return ToolLoopAction{}, nil
}

func (c *stopAfterController) seen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rounds)
}

func countingToolAgent(t *testing.T, provider model.Provider, calls *int) *Agent {
	t.Helper()
	a, err := New("worker", "Worker").WithModel(provider).WithMaxIterations(2).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Tools.Register(&tool.Definition{Name: "search", Permission: tool.PermAllow, Handler: func(context.Context, map[string]any) (any, error) {
		*calls++
		return "result", nil
	}})
	return a
}

func toolRounds(n int, final string) []*model.ChatResponse {
	replies := make([]*model.ChatResponse, 0, n+1)
	for i := 0; i < n; i++ {
		replies = append(replies, toolCallResp("t"+string(rune('a'+i)), "search"))
	}
	return append(replies, &model.ChatResponse{StopReason: model.StopReasonEnd, Content: final})
}

func TestToolLoopControllerReplacesFixedIterationCap(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(5, "done")}, &calls)
	controller := &stopAfterController{}
	ctx := WithToolLoopController(context.Background(), a.ID, controller)

	resp, err := a.Chat(ctx, "go")
	if err != nil {
		t.Fatalf("Chat() error = %v, want the controller to own bounding", err)
	}
	if resp.Content != "done" || calls != 5 || controller.seen() != 5 {
		t.Fatalf("content = %q, tool calls = %d, rounds seen = %d", resp.Content, calls, controller.seen())
	}
	if controller.rounds[4].Iteration != 5 || controller.rounds[0].AgentID != a.ID || len(controller.rounds[0].ToolCalls) != 1 {
		t.Fatalf("round metadata = %+v", controller.rounds)
	}
}

func TestToolLoopControllerStopReturnsPausedResponseAfterResults(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(5, "done")}, &calls)
	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{stopAt: 2})

	resp, err := a.Chat(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != model.StopReasonPaused || resp.Content != "paused: no progress" || calls != 2 {
		t.Fatalf("response = %+v, tool calls = %d", resp, calls)
	}
}

func TestToolLoopControllerIsBoundToOneAgent(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(5, "done")}, &calls)
	ctx := WithToolLoopController(context.Background(), "someone-else", &stopAfterController{})

	if _, err := a.Chat(ctx, "go"); err == nil || !strings.Contains(err.Error(), "exceeded max tool-calling iterations (2)") {
		t.Fatalf("Chat() error = %v, want the fixed cap for an unbound agent", err)
	}
}

func TestStreamToolLoopControllerStopFollowsStreamProtocol(t *testing.T) {
	round := []*model.ChatResponse{{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "search", Arguments: `{}`}}, Usage: model.Usage{PromptTokens: 7, CompletionTokens: 1}, StopReason: model.StopReasonToolCall}}
	prov := &streamProvider{scripts: [][]*model.ChatResponse{round, round, round}}
	calls := 0
	a := countingToolAgent(t, prov, &calls)
	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{stopAt: 3})

	ch, err := a.ChatStream(ctx, "go")
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var final *model.ChatResponse
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error = %v", chunk.Err)
		}
		if chunk.Delta {
			text.WriteString(chunk.Content)
			continue
		}
		final = chunk
	}
	if final == nil || final.StopReason != model.StopReasonPaused || text.String() != "paused: no progress" || calls != 3 || prov.calls != 3 {
		t.Fatalf("final = %+v, text = %q, tool calls = %d, model calls = %d", final, text.String(), calls, prov.calls)
	}
	if final.Usage.PromptTokens != 21 {
		t.Fatalf("aggregated usage = %+v, want every completed round", final.Usage)
	}
}

func sessionRoles(t *testing.T, store *testStorage, sessionID string) []string {
	t.Helper()
	events, err := store.ListEvents(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	cs := chatSessionFromEvents(events)
	roles := make([]string, len(cs.Messages))
	for i, msg := range cs.Messages {
		roles[i] = msg.Role
	}
	return roles
}

func TestSessionPersistsToolRoundsSoPausedWorkSurvivesTheTurn(t *testing.T) {
	calls := 0
	prov := &recordingProvider{replies: toolRounds(1, "resumed")}
	a := countingToolAgent(t, prov, &calls)
	store := newTestStorage()
	a.Storage = store
	a.ContextCfg.PersistToolRounds = true

	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{stopAt: 1})
	resp, err := a.ChatWithSession(ctx, "s1", "do the task")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != model.StopReasonPaused {
		t.Fatalf("stop reason = %q", resp.StopReason)
	}
	want := []string{model.RoleUser, model.RoleAssistant, model.RoleTool, model.RoleAssistant}
	if got := sessionRoles(t, store, "s1"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("persisted roles = %v, want %v", got, want)
	}

	if _, err := a.ChatWithSession(context.Background(), "s1", "continue"); err != nil {
		t.Fatal(err)
	}
	next := prov.roleSeqs[1]
	if strings.Join(next, ",") != strings.Join(append(append([]string(nil), want...), model.RoleUser), ",") {
		t.Fatalf("next turn request roles = %v, want the paused round in context", next)
	}
}

func TestSessionWithoutPersistToolRoundsKeepsLegacyLedger(t *testing.T) {
	calls := 0
	a := countingToolAgent(t, &recordingProvider{replies: toolRounds(1, "done")}, &calls)
	store := newTestStorage()
	a.Storage = store

	if _, err := a.ChatWithSession(context.Background(), "s1", "task"); err != nil {
		t.Fatal(err)
	}
	if got := sessionRoles(t, store, "s1"); strings.Join(got, ",") != "user,assistant" {
		t.Fatalf("persisted roles = %v", got)
	}
}

func TestStreamSessionPersistsRoundsAndPausedMessage(t *testing.T) {
	round := []*model.ChatResponse{{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "search", Arguments: `{}`}}, StopReason: model.StopReasonToolCall}}
	prov := &streamProvider{scripts: [][]*model.ChatResponse{round}}
	calls := 0
	a := countingToolAgent(t, prov, &calls)
	store := newTestStorage()
	a.Storage = store
	a.ContextCfg.PersistToolRounds = true

	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{stopAt: 1})
	ch, err := a.ChatStreamWithSession(ctx, "s1", "task")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := collectStream(t, ch); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListEvents(context.Background(), "s1", 0)
	cs := chatSessionFromEvents(events)
	if len(cs.Messages) != 4 || cs.Messages[3].Content != "paused: no progress" || cs.Messages[1].ToolCalls[0].ID != "1" {
		t.Fatalf("persisted conversation = %+v", cs.Messages)
	}
}

func TestRepairToolPairsKeepsOnlyCompleteRounds(t *testing.T) {
	call := func(ids ...string) model.Message {
		msg := model.Message{Role: model.RoleAssistant, Content: "thinking"}
		for _, id := range ids {
			msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{ID: id, Name: "read"})
		}
		return msg
	}
	result := func(id string) model.Message { return model.Message{Role: model.RoleTool, ToolCallID: id} }
	got := repairToolPairs([]model.Message{
		result("orphan"),
		{Role: model.RoleUser, Content: "task"},
		call("a", "b"), result("a"), result("b"),
		call("c", "d"), result("c"), // crash before d was persisted
		{Role: model.RoleUser, Content: "continue"},
	})
	var roles []string
	for _, msg := range got {
		roles = append(roles, msg.Role)
	}
	if strings.Join(roles, ",") != "user,assistant,tool,tool,assistant,user" {
		t.Fatalf("roles = %v", roles)
	}
	if len(got[4].ToolCalls) != 0 || got[4].Content != "thinking" {
		t.Fatalf("incomplete round = %+v, want its text without calls", got[4])
	}
}

// compactingProvider answers summarization requests with a fixed summary and
// otherwise requests one large-output tool call per round until rounds run out.
type compactingProvider struct {
	mu        sync.Mutex
	rounds    int
	summaries int
	requests  [][]model.Message
}

func (p *compactingProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "conversation summarizer") {
		p.summaries++
		return &model.ChatResponse{Content: "summary of earlier work", StopReason: model.StopReasonEnd}, nil
	}
	p.requests = append(p.requests, append([]model.Message(nil), req.Messages...))
	if p.rounds == 0 {
		return &model.ChatResponse{Content: "finished", StopReason: model.StopReasonEnd}, nil
	}
	p.rounds--
	id := "call-" + string(rune('a'+p.rounds))
	return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: id, Name: "search", Arguments: `{}`}}}, nil
}

func (p *compactingProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, nil
}
func (p *compactingProvider) Name() string  { return "compacting" }
func (p *compactingProvider) Model() string { return "compacting-model" }

func TestSessionCompactsInsideLongTurnAndKeepsCurrentTask(t *testing.T) {
	prov := &compactingProvider{rounds: 12}
	a, err := New("worker", "Worker").WithModel(prov).Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Tools.Register(&tool.Definition{Name: "search", Permission: tool.PermAllow, Handler: func(context.Context, map[string]any) (any, error) {
		return strings.Repeat("large tool output ", 60), nil
	}})
	store := newTestStorage()
	a.Storage = store
	a.ContextCfg = ContextConfig{PersistToolRounds: true, MaxContextTokens: 2000, SummarizeThreshold: 0.5, PreserveRecentTurns: 1}
	ctx := WithToolLoopController(context.Background(), a.ID, &stopAfterController{})

	resp, err := a.ChatWithSession(ctx, "s1", "THE TASK")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "finished" || prov.summaries == 0 {
		t.Fatalf("content = %q, summaries = %d, want in-turn compaction", resp.Content, prov.summaries)
	}
	last := prov.requests[len(prov.requests)-1]
	var sawSummary, sawTask bool
	for _, msg := range last {
		sawSummary = sawSummary || strings.Contains(msg.Content, "summary of earlier work")
		sawTask = sawTask || msg.Content == "THE TASK"
	}
	if !sawSummary || !sawTask {
		t.Fatalf("final request summary = %v, task = %v: %+v", sawSummary, sawTask, last)
	}
	events, _ := store.ListEvents(context.Background(), "s1", 0)
	cs := chatSessionFromEvents(events)
	if cs.Summary != "summary of earlier work" || len(cs.Messages) == 0 {
		t.Fatalf("restored session summary = %q, messages = %d", cs.Summary, len(cs.Messages))
	}
	for _, msg := range cs.Messages {
		if msg.Content == "THE TASK" {
			return
		}
	}
	t.Fatalf("restored session lost the current task: %+v", cs.Messages)
}
