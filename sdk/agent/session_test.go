package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/guardrails"
	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
	"github.com/spawn08/chronos/engine/tool"
	chronostrace "github.com/spawn08/chronos/os/trace"
	"github.com/spawn08/chronos/storage"
	"github.com/spawn08/chronos/storage/adapters/sqlite"
)

func TestPersistMessage_Basic(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{Role: model.RoleUser, Content: "hello"}
	err := persistMessage(context.Background(), store, "sess-1", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage: %v", err)
	}
	evts := store.events["sess-1"]
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type != "chat_message" {
		t.Errorf("unexpected type: %q", evts[0].Type)
	}
}

func TestPersistMessage_WithToolCalls(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{
		Role:    model.RoleAssistant,
		Content: "",
		ToolCalls: []model.ToolCall{
			{ID: "tc-1", Name: "my_tool", Arguments: `{"x":1}`},
		},
	}
	err := persistMessage(context.Background(), store, "sess-2", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage with tool calls: %v", err)
	}
	evts := store.events["sess-2"]
	payload, ok := evts[0].Payload.(map[string]any)
	if !ok {
		t.Fatal("expected map payload")
	}
	if _, ok := payload["tool_calls"]; !ok {
		t.Error("expected tool_calls in payload")
	}
}

func TestPersistMessage_WithNameAndToolCallID(t *testing.T) {
	store := newTestStorage()
	msg := model.Message{
		Role:       model.RoleTool,
		Content:    "result",
		Name:       "my_tool",
		ToolCallID: "tc-1",
	}
	err := persistMessage(context.Background(), store, "sess-3", 1, msg)
	if err != nil {
		t.Fatalf("persistMessage: %v", err)
	}
	evts := store.events["sess-3"]
	payload, _ := evts[0].Payload.(map[string]any)
	if payload["name"] != "my_tool" {
		t.Errorf("expected name=my_tool, got %v", payload["name"])
	}
	if payload["tool_call_id"] != "tc-1" {
		t.Errorf("expected tool_call_id=tc-1, got %v", payload["tool_call_id"])
	}
}

func TestPersistSummary(t *testing.T) {
	store := newTestStorage()
	err := persistSummary(context.Background(), store, "sess-sum", 1, "this is a summary")
	if err != nil {
		t.Fatalf("persistSummary: %v", err)
	}
	evts := store.events["sess-sum"]
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type != "chat_summary" {
		t.Errorf("unexpected type: %q", evts[0].Type)
	}
}

func TestCompactSession_NoModel(t *testing.T) {
	a := &Agent{ID: "a1"}
	err := a.CompactSession(context.Background(), "sess")
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("expected 'no model' error, got %v", err)
	}
}

func TestCompactSession_NoStorage(t *testing.T) {
	a, err := New("a1", "T").WithModel(&fakeChatProvider{content: "summary"}).Build()
	if err != nil {
		t.Fatal(err)
	}
	err = a.CompactSession(context.Background(), "sess")
	if err == nil || !strings.Contains(err.Error(), "no storage") {
		t.Fatalf("expected 'no storage' error, got %v", err)
	}
}

func TestCompactSession_EmptySessionIsNoop(t *testing.T) {
	store := newTestStorage()
	a, err := New("a1", "T").WithModel(&fakeChatProvider{content: "summary"}).WithStorage(store).Build()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.CompactSession(context.Background(), "no-such-session"); err != nil {
		t.Fatalf("expected no-op nil error for an empty session, got %v", err)
	}
}

func TestCompactSession_SummarizesRegardlessOfContextSize(t *testing.T) {
	store := newTestStorage()
	sid := "compact-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "investigate the 7 bottlenecks"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "found bottleneck 1"}},
		{ID: "e3", SessionID: sid, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "keep going"}},
		{ID: "e4", SessionID: sid, SeqNum: 4, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "found bottleneck 2"}},
	}

	// PreserveRecentTurns: 1 keeps only the last 2 messages verbatim, so with
	// 4 messages here there's still something older to actually summarize.
	// This session's real conversation is tiny either way, nowhere near any
	// context-window threshold, so the automatic inline compaction in
	// ChatWithSession would never fire on its own. CompactSession must
	// summarize anyway, since it's being asked to recover from an unrelated
	// failure (a budget cap), not a context-window limit.
	a, err := New("a1", "T").
		WithModel(&fakeChatProvider{content: "rolled-up summary"}).
		WithStorage(store).
		WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	if err := a.CompactSession(context.Background(), sid); err != nil {
		t.Fatalf("CompactSession: %v", err)
	}

	events := store.events[sid]
	last := events[len(events)-1]
	if last.Type != "chat_summary" {
		t.Fatalf("expected a chat_summary event to be appended, last event type = %q", last.Type)
	}
	payload := last.Payload.(map[string]any)
	if payload["summary"] != "rolled-up summary" {
		t.Errorf("summary = %v, want %q", payload["summary"], "rolled-up summary")
	}
}

func TestCompactSession_SummarizeErrorPropagates(t *testing.T) {
	store := newTestStorage()
	sid := "compact-err-sess"
	store.sessions[sid] = &storage.Session{ID: sid, AgentID: "a1", Status: "active"}
	store.events[sid] = []*storage.Event{
		{ID: "e1", SessionID: sid, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "hi"}},
		{ID: "e2", SessionID: sid, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "hello"}},
		{ID: "e3", SessionID: sid, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "continue"}},
	}

	a, err := New("a1", "T").
		WithModel(&fakeChatProvider{err: errors.New("model unavailable")}).
		WithStorage(store).
		WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	err = a.CompactSession(context.Background(), sid)
	if err == nil || !strings.Contains(err.Error(), "summarize") {
		t.Fatalf("expected summarize error, got %v", err)
	}
}

// fakeChatProvider is a minimal model.Provider stub for CompactSession tests
// that don't need the multi-reply sequencing seqTestProvider provides.
type fakeChatProvider struct {
	content string
	err     error
}

func (f *fakeChatProvider) Chat(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.ChatResponse{Content: f.content, StopReason: model.StopReasonEnd}, nil
}
func (f *fakeChatProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeChatProvider) Name() string  { return "fake" }
func (f *fakeChatProvider) Model() string { return "fake-model" }

func TestChatWithSession_NoModel(t *testing.T) {
	a := &Agent{ID: "a1"}
	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error for no model")
		return
	}
}

func TestChatWithSession_NoStorage(t *testing.T) {
	a := &Agent{
		ID:    "a1",
		Model: &testProvider{response: &model.ChatResponse{Content: "hi"}},
	}
	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error for no storage")
		return
	}
}

func TestChatWithSession_Success(t *testing.T) {
	store := newTestStorage()
	prov := &testProvider{response: &model.ChatResponse{Content: "hello back", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "test-session", "hello")
	if err != nil {
		t.Fatalf("ChatWithSession: %v", err)
	}
	if resp.Content != "hello back" {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestSessionChatPassesNativeReasoningWithTools(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("stream=%v/think=%v", streaming, enabled), func(t *testing.T) {
				var provider model.Provider
				var request *model.ChatRequest
				if streaming {
					provider = &streamProvider{scripts: [][]*model.ChatResponse{{
						{Role: model.RoleAssistant, Content: "ok", Delta: true},
						{Role: model.RoleAssistant, StopReason: model.StopReasonEnd},
					}}}
				} else {
					provider = &testProvider{response: &model.ChatResponse{Content: "ok", StopReason: model.StopReasonEnd}}
				}
				a, err := New("a1", "Test").WithModel(provider).WithStorage(newTestStorage()).WithReasoningConfig(model.ReasoningConfig{Enabled: enabled, Effort: "high"}).Build()
				if err != nil {
					t.Fatal(err)
				}
				a.Tools.Register(&tool.Definition{Name: "lookup", Handler: func(context.Context, map[string]any) (any, error) { return nil, nil }})
				if streaming {
					ch, err := a.ChatStreamWithSession(t.Context(), "sess", "hi")
					if err != nil {
						t.Fatal(err)
					}
					if _, _, err := collectStream(t, ch); err != nil {
						t.Fatal(err)
					}
					request = provider.(*streamProvider).requests[0]
				} else {
					if _, err := a.ChatWithSession(t.Context(), "sess", "hi"); err != nil {
						t.Fatal(err)
					}
					request = provider.(*testProvider).lastReq
				}
				if len(request.Tools) != 1 || (request.Reasoning != nil) != enabled {
					t.Fatalf("session request tools/reasoning = %+v / %+v", request.Tools, request.Reasoning)
				}
				if enabled && request.Reasoning.Effort != "high" {
					t.Fatalf("reasoning effort = %q, want high", request.Reasoning.Effort)
				}
			})
		}
	}
}

func TestChatWithSession_ExistingSession(t *testing.T) {
	store := newTestStorage()
	// Pre-create session so GetSession succeeds
	store.sessions["existing-sess"] = &storage.Session{ID: "existing-sess", AgentID: "a1", Status: "active"}
	// Add a prior event
	store.events["existing-sess"] = []*storage.Event{
		{
			ID: "e1", SessionID: "existing-sess", SeqNum: 1, Type: "chat_message",
			Payload: map[string]any{"role": "user", "content": "prior message"},
		},
	}
	prov := &testProvider{response: &model.ChatResponse{Content: "reply", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "existing-sess", "follow-up")
	if err != nil {
		t.Fatalf("ChatWithSession with existing session: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
		return
	}
}

func TestChatWithSession_ModelError(t *testing.T) {
	store := newTestStorage()
	prov := &testProvider{err: errors.New("model failed")}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	_, err := a.ChatWithSession(context.Background(), "sess", "hello")
	if err == nil {
		t.Fatal("expected error from model failure")
		return
	}
}

func TestChatWithSession_WithSummary(t *testing.T) {
	store := newTestStorage()
	// Pre-create session with a summary event
	store.sessions["sum-sess"] = &storage.Session{ID: "sum-sess", AgentID: "a1", Status: "active"}
	store.events["sum-sess"] = []*storage.Event{
		{
			ID: "e1", SessionID: "sum-sess", SeqNum: 1, Type: "chat_summary",
			Payload: "This is a prior summary.",
		},
	}
	prov := &testProvider{response: &model.ChatResponse{Content: "continuing after summary", StopReason: model.StopReasonEnd}}
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()

	resp, err := a.ChatWithSession(context.Background(), "sum-sess", "continue")
	if err != nil {
		t.Fatalf("ChatWithSession with summary: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
		return
	}
}

func TestBuildSystemContext_WithSystemPromptAndInstructions(t *testing.T) {
	a := &Agent{
		SystemPrompt: "You are helpful",
		Instructions: []string{"Be concise", "Use simple language"},
	}
	msgs := a.buildSystemContext(context.Background(), "test query")
	// 1 system prompt + 2 instructions
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "You are helpful" {
		t.Errorf("unexpected system prompt: %q", msgs[0].Content)
	}
}

func TestBuildSystemContext_Empty(t *testing.T) {
	a := &Agent{}
	msgs := a.buildSystemContext(context.Background(), "test")
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages, got %d", len(msgs))
	}
}

func TestResolveContextLimit_WithConfig(t *testing.T) {
	a := &Agent{
		Model:      &testProvider{},
		ContextCfg: ContextConfig{MaxContextTokens: 8000},
	}
	limit := a.resolveContextLimit()
	if limit != 8000 {
		t.Errorf("expected 8000, got %d", limit)
	}
}

func TestResolveContextLimit_Default(t *testing.T) {
	a := &Agent{
		Model:      &testProvider{},
		ContextCfg: ContextConfig{},
	}
	limit := a.resolveContextLimit()
	// Should return some non-zero default from model.ContextLimit
	if limit <= 0 {
		t.Errorf("expected positive limit, got %d", limit)
	}
}

func TestBuildSystemContext_WithKnowledge(t *testing.T) {
	a := &Agent{
		SystemPrompt: "helpful assistant",
		Knowledge:    &mockKnowledge{},
	}
	msgs := a.buildSystemContext(context.Background(), "search query")
	// Should have system prompt + knowledge context
	if len(msgs) < 2 {
		t.Errorf("expected at least 2 messages (prompt + knowledge), got %d", len(msgs))
	}
	hasKnowledge := false
	for _, m := range msgs {
		if len(m.Content) > 10 && m.Role == model.RoleSystem {
			if m.Content != "helpful assistant" {
				hasKnowledge = true
			}
		}
	}
	if !hasKnowledge {
		t.Error("expected knowledge context in messages")
	}
}

// BL-004: checkpoints survive a real storage close/reopen without rewriting the ledger.
func TestSessionCheckpoint_RepeatedCompactionRestart(t *testing.T) {
	ctx := storage.WithTenant(context.Background(), "tenant-a")
	sid := "checkpoint-restart"
	path := filepath.Join(t.TempDir(), "session.db")
	open := func() *sqlite.Store {
		t.Helper()
		s, err := sqlite.New(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		return s
	}
	store := open()
	old := []model.Message{{Role: model.RoleUser, Content: "old question"}, {Role: model.RoleAssistant, Content: "old answer"}}
	tail := []model.Message{
		{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"a.go"}`}}},
		{Role: model.RoleTool, Name: "read", ToolCallID: "call-1", Content: "recent tool result"},
	}
	for i, msg := range append(old, tail...) {
		if err := persistMessage(ctx, store, sid, int64((i+1)*10), msg); err != nil {
			t.Fatal(err)
		}
	}
	if err := persistSummary(ctx, store, sid, 50, "legacy overview"); err != nil {
		t.Fatal(err)
	}
	// A non-chat event with a gap must also count toward the high-water mark.
	if err := store.AppendEvent(ctx, &storage.Event{ID: "audit-90", SessionID: sid, SeqNum: 90, Type: "audit", Payload: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	original, err := store.ListEvents(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	provider := &checkpointProvider{}
	compact := func(s storage.Storage) {
		t.Helper()
		a, err := New("a", "A").WithModel(provider).WithStorage(s).
			WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).Build()
		if err != nil {
			t.Fatal(err)
		}
		if err := a.CompactSession(ctx, sid); err != nil {
			t.Fatal(err)
		}
	}
	compact(store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = open()
	events, err := store.ListEvents(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	cs := chatSessionFromEvents(events)
	if !reflect.DeepEqual(cs.Messages, tail) || cs.Summary != "summary-1" {
		t.Fatalf("restart restored %#v, summary %q; want only preserved tool tail", cs.Messages, cs.Summary)
	}
	if events[len(events)-1].SeqNum != 91 {
		t.Fatalf("checkpoint sequence = %d, want 91", events[len(events)-1].SeqNum)
	}
	payload := events[len(events)-1].Payload.(map[string]any)
	if payload["version"] != float64(1) || payload["covered_seq"] != float64(90) || payload["preserved_messages"] == nil {
		t.Fatalf("checkpoint payload = %#v", payload)
	}
	if !strings.Contains(provider.summaryInputs[0], "legacy overview") {
		t.Fatalf("legacy summary missing from compaction: %q", provider.summaryInputs[0])
	}
	// Recompacting just the retained tail is a summarizer no-op, not data loss.
	compact(store)
	newTail := []model.Message{{Role: model.RoleUser, Content: "new question"}, {Role: model.RoleAssistant, Content: "new answer"}}
	for i, msg := range newTail {
		if err := persistMessage(ctx, store, sid, int64(100+i), msg); err != nil {
			t.Fatal(err)
		}
	}
	compact(store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = open()
	events, err = store.ListEvents(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	cs = chatSessionFromEvents(events)
	if !reflect.DeepEqual(cs.Messages, newTail) || cs.Summary != "summary-2" {
		t.Fatalf("second restart = %#v, summary %q", cs.Messages, cs.Summary)
	}
	if provider.summaries != 2 || !strings.Contains(provider.summaryInputs[1], "summary-1") || strings.Contains(provider.summaryInputs[1], "old question") {
		t.Fatalf("recompaction inputs = %#v", provider.summaryInputs)
	}
	if len(events) != len(original)+5 || !reflect.DeepEqual(events[:len(original)], original) {
		t.Fatal("compaction changed the historical ledger")
	}
	for i := 1; i < len(events); i++ {
		if events[i].SeqNum <= events[i-1].SeqNum {
			t.Fatalf("non-increasing sequences at %d", i)
		}
	}
	other, err := store.ListEvents(storage.WithTenant(context.Background(), "tenant-b"), sid, 0)
	if err != nil || len(other) != 0 {
		t.Fatalf("other tenant events = %v, err = %v", other, err)
	}
}

// BL-004: both automatic compaction entry points must persist the same boundary.
func TestSessionCheckpoint_AutomaticParity(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprintf("streaming=%t", streaming), func(t *testing.T) {
			ctx := context.Background()
			store := newTestStorage()
			sid := "automatic"
			for i, content := range []string{"old user", "old assistant", "recent user", "recent assistant"} {
				role := model.RoleUser
				if i%2 == 1 {
					role = model.RoleAssistant
				}
				if err := persistMessage(ctx, store, sid, int64(10+i), model.Message{Role: role, Content: content}); err != nil {
					t.Fatal(err)
				}
			}
			provider := &checkpointProvider{}
			a, err := New("a", "A").WithModel(provider).WithStorage(store).
				WithContextConfig(ContextConfig{MaxContextTokens: 4096, SummarizeThreshold: 0.001, PreserveRecentTurns: 1}).Build()
			if err != nil {
				t.Fatal(err)
			}
			call := func(a *Agent, input string) {
				t.Helper()
				if streaming {
					ch, err := a.ChatStreamWithSession(ctx, sid, input)
					if err != nil {
						t.Fatal(err)
					}
					var last *model.ChatResponse
					var content strings.Builder
					for resp := range ch {
						if resp.Err != nil {
							t.Fatal(resp.Err)
						}
						content.WriteString(resp.Content)
						last = resp
					}
					if last == nil || last.StopReason != model.StopReasonEnd || content.String() != "reply" {
						t.Fatalf("stream response = %#v", last)
					}
				} else if _, err := a.ChatWithSession(ctx, sid, input); err != nil {
					t.Fatal(err)
				}
			}
			call(a, "current user")
			want := []model.Message{{Role: model.RoleAssistant, Content: "recent assistant"}, {Role: model.RoleUser, Content: "current user"}, {Role: model.RoleAssistant, Content: "reply"}}
			cs := chatSessionFromEvents(store.events[sid])
			if !reflect.DeepEqual(cs.Messages, want) || cs.Summary != "summary-1" {
				t.Fatalf("replay = %#v, summary %q", cs.Messages, cs.Summary)
			}
			for i, event := range store.events[sid] {
				if event.SeqNum != int64(10+i) {
					t.Fatalf("sequence[%d] = %d, want %d", i, event.SeqNum, 10+i)
				}
			}
			// A new agent resumes the checkpoint and the post-checkpoint response.
			a, err = New("a", "A").WithModel(provider).WithStorage(store).Build()
			if err != nil {
				t.Fatal(err)
			}
			call(a, "after restart")
			wantRequest := append([]model.Message{{Role: model.RoleSystem, Content: "Previous conversation summary:\nsummary-1"}}, want...)
			wantRequest = append(wantRequest, model.Message{Role: model.RoleUser, Content: "after restart"})
			if !reflect.DeepEqual(provider.requests[len(provider.requests)-1], wantRequest) {
				t.Fatalf("resumed request = %#v", provider.requests[len(provider.requests)-1])
			}
			if len(store.events[sid]) != 9 {
				t.Fatalf("ledger contains %d events, want 9", len(store.events[sid]))
			}
		})
	}
}

func TestSessionCheckpoint_LegacyAndInvalid(t *testing.T) {
	for _, fields := range []map[string]any{
		{}, // legacy summary: no safe cutoff can be inferred
		{"version": 2, "covered_seq": 1, "preserved_messages": []any{}},
		{"version": 1, "covered_seq": 99, "preserved_messages": []any{}},
		{"version": 1, "covered_seq": 1},
		{"version": 1, "covered_seq": 1, "preserved_messages": "invalid"},
	} {
		fields["summary"] = "legacy summary"
		events := []*storage.Event{
			{SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "retained"}},
			{SeqNum: 2, Type: "chat_summary", Payload: fields},
		}
		cs := chatSessionFromEvents(events)
		if len(cs.Messages) != 1 || cs.Messages[0].Content != "retained" || cs.Summary != "legacy summary" {
			t.Fatalf("unsafe replay for %#v: %#v", fields, cs)
		}
	}
}

func TestSessionCheckpoint_EmptyTailAndCoveredBoundary(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage()
	if err := persistMessage(ctx, store, "s", 1, model.Message{Role: model.RoleUser, Content: "covered"}); err != nil {
		t.Fatal(err)
	}
	// This message is newer than the checkpoint's coverage, even though its
	// sequence precedes the checkpoint event itself. It must still be replayed.
	later := model.Message{Role: model.RoleUser, Content: "outside coverage"}
	if err := persistMessage(ctx, store, "s", 2, later); err != nil {
		t.Fatal(err)
	}
	if err := persistSummaryCheckpoint(ctx, store, "s", 3, 1, model.SummarizationResult{Summary: "all older messages summarized"}); err != nil {
		t.Fatal(err)
	}
	cs := chatSessionFromEvents(store.events["s"])
	if !reflect.DeepEqual(cs.Messages, []model.Message{later}) || cs.Summary != "all older messages summarized" {
		t.Fatalf("replay = %#v", cs)
	}
	if err := persistSummaryCheckpoint(ctx, store, "s", 4, 4, model.SummarizationResult{}); err == nil {
		t.Fatal("accepted a checkpoint covering its own sequence")
	}
	if len(store.events["s"]) != 3 {
		t.Fatal("invalid checkpoint appended an event")
	}
}

func TestSessionCheckpoint_AppendFailureRetainsHistory(t *testing.T) {
	for _, mode := range []string{"forced", "blocking", "streaming"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			base := newTestStorage()
			for i := 1; i <= 4; i++ {
				if err := persistMessage(ctx, base, "s", int64(i), model.Message{Role: model.RoleUser, Content: fmt.Sprintf("message-%d", i)}); err != nil {
					t.Fatal(err)
				}
			}
			sentinel := errors.New("checkpoint write failed")
			provider := &checkpointProvider{}
			a, err := New("a", "A").WithModel(provider).
				WithStorage(&checkpointFailStorage{Storage: base, err: sentinel}).
				WithContextConfig(ContextConfig{MaxContextTokens: 4096, SummarizeThreshold: 0.001, PreserveRecentTurns: 1}).Build()
			if err != nil {
				t.Fatal(err)
			}
			wantCount := 5 // the new user turn is persisted before compaction
			switch mode {
			case "forced":
				err = a.CompactSession(ctx, "s")
				wantCount = 4
			case "blocking":
				_, err = a.ChatWithSession(ctx, "s", "new turn")
			case "streaming":
				_, err = a.ChatStreamWithSession(ctx, "s", "new turn")
			}
			if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "persist summary") {
				t.Fatalf("error = %v", err)
			}
			cs := chatSessionFromEvents(base.events["s"])
			if len(cs.Messages) != wantCount || cs.Summary != "" || len(provider.requests) != 0 {
				t.Fatalf("failed checkpoint changed active history or called chat: %#v", cs)
			}
		})
	}
}

type checkpointFailStorage struct {
	storage.Storage
	err error
}

func (s *checkpointFailStorage) AppendEvent(ctx context.Context, event *storage.Event) error {
	if event.Type == "chat_summary" {
		return s.err
	}
	return s.Storage.AppendEvent(ctx, event)
}

type checkpointProvider struct {
	summaries     int
	summaryInputs []string
	requests      [][]model.Message
}

func (p *checkpointProvider) Chat(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	if len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "conversation summarizer") {
		p.summaries++
		p.summaryInputs = append(p.summaryInputs, req.Messages[1].Content)
		return &model.ChatResponse{Content: fmt.Sprintf("summary-%d", p.summaries), StopReason: model.StopReasonEnd}, nil
	}
	p.requests = append(p.requests, append([]model.Message(nil), req.Messages...))
	return &model.ChatResponse{Content: "reply", StopReason: model.StopReasonEnd}, nil
}

func (p *checkpointProvider) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	resp, err := p.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *model.ChatResponse, 1)
	ch <- resp
	close(ch)
	return ch, nil
}

func (p *checkpointProvider) Name() string  { return "checkpoint-test" }
func (p *checkpointProvider) Model() string { return "test-model" }

// BL-004: the initial session request retries inside a single persisted turn,
// with one budget reservation/reconciliation and one model trace/event.
func TestChatWithSession_InitialRequestRecovery(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(fmt.Sprintf("succeeds=%t", succeeds), func(t *testing.T) {
			ctx := context.Background()
			sid := "initial-recovery"
			store := newTestStorage()
			broker := stream.NewBroker()
			t.Cleanup(func() { _ = broker.Close() })
			sub, err := broker.SubscribeTopic(sid)
			if err != nil {
				t.Fatal(err)
			}
			calls, starts, reservations, reconciliations := 0, 0, 0, 0
			var request *model.ChatRequest
			var accounted model.Usage
			usage := model.Usage{PromptTokens: 12, CompletionTokens: 3, ContextTokens: 12}
			sentinel := &model.APIError{StatusCode: 503, Status: "unavailable"}
			p := &sessionRecoveryProvider{modelID: "live-model", chat: func(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				if storage.SessionFromContext(ctx) != sid || req.Model != "live-model" || req.ResponseFormat != "json_schema" {
					t.Errorf("lost session/live model/schema: session=%q request=%#v", storage.SessionFromContext(ctx), req)
				}
				if len(store.events[sid]) != 1 || store.events[sid][0].Payload.(map[string]any)["role"] != model.RoleUser {
					t.Errorf("attempt %d must see exactly one user event: %#v", calls, store.events[sid])
				}
				if request != nil && request != req {
					t.Error("retry rebuilt the model request")
				}
				request = req
				if calls == 1 || !succeeds {
					return nil, sentinel
				}
				return &model.ChatResponse{Content: `{"ok":true}`, StopReason: model.StopReasonEnd, Usage: usage}, nil
			}}
			h := sessionRecoveryHook{before: func(_ context.Context, e *hooks.Event) error {
				switch e.Type {
				case hooks.EventSessionStart:
					starts++
				case hooks.EventModelCallBefore:
					reservations++
					if e.Input.(*model.ChatRequest).Model != "live-model" || e.Metadata["correlation_id"] == nil {
						t.Errorf("hook missing live model/correlation: %#v", e)
					}
					e.Metadata["reservation"] = "reserved"
				}
				return nil
			}, after: func(_ context.Context, e *hooks.Event) error {
				if e.Type == hooks.EventModelCallAfter {
					reconciliations++
					if e.Metadata["reservation"] != "reserved" {
						t.Error("reservation lost or reconciled twice")
					}
					delete(e.Metadata, "reservation")
					if e.Error == nil {
						accounted = e.Output.(*model.ChatResponse).Usage
					}
				}
				return nil
			}}
			a, err := New("a", "A").WithModel(p).WithStorage(store).WithBroker(broker).
				WithTracer(chronostrace.NewCollector(store)).AddHook(h).
				WithOutputSchema(map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}}).Build()
			if err != nil {
				t.Fatal(err)
			}
			resp, err := a.ChatWithSession(ctx, sid, "hello")
			wantEvents := 1
			if succeeds {
				wantEvents = 2
				if err != nil || resp == nil || resp.Content != `{"ok":true}` || resp.Usage != usage || accounted != usage {
					t.Fatalf("response=%#v err=%v accounted=%#v", resp, err, accounted)
				}
			} else if !errors.Is(err, sentinel) || accounted != (model.Usage{}) {
				t.Fatalf("error=%v accounted=%#v", err, accounted)
			}
			if calls != 2 || starts != 1 || reservations != 1 || reconciliations != 1 || len(store.events[sid]) != wantEvents {
				t.Fatalf("calls=%d starts=%d reservation/reconciliation=%d/%d ledger=%d", calls, starts, reservations, reconciliations, len(store.events[sid]))
			}
			if len(store.traces) != 1 {
				t.Fatalf("model traces=%d, want 1", len(store.traces))
			}
			for _, trace := range store.traces {
				if trace.SessionID != sid || trace.EndedAt.IsZero() || (trace.Error == "") != succeeds {
					t.Errorf("incomplete/incorrect trace: %#v", trace)
				}
			}
			modelEvents := 0
			for len(sub.C) > 0 {
				if e := <-sub.C; e.Type == stream.EventModelCall {
					modelEvents++
				}
			}
			if modelEvents != 1 {
				t.Fatalf("model broker events=%d, want 1", modelEvents)
			}
		})
	}
}

// BL-003/004: hook replacement of the initial request slice must become the
// working history for the next tool round, without deleting persisted history.
func TestChatWithSession_InitialTrimAndLiveModel(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage()
	sid := "trimmed-session"
	for i, msg := range []model.Message{{Role: model.RoleUser, Content: "old question"}, {Role: model.RoleAssistant, Content: "old answer"}} {
		if err := persistMessage(ctx, store, sid, int64(i+1), msg); err != nil {
			t.Fatal(err)
		}
	}
	calls, mutations, before, after := 0, 0, 0, 0
	p := &sessionRecoveryProvider{modelID: "initial-live"}
	follow := &sessionRecoveryProvider{modelID: "follow-live"}
	chat := func(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
		calls++
		want := []model.Message{{Role: model.RoleUser, Content: "current task"}}
		wantModel := "initial-live"
		if calls > 1 {
			want = append(want,
				model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "1", Name: "mutate", Arguments: `{}`}}},
				model.Message{Role: model.RoleTool, Name: "mutate", ToolCallID: "1", Content: `"committed"`})
		}
		if req.Model != wantModel || !reflect.DeepEqual(req.Messages, want) {
			t.Errorf("round %d request model=%q messages=%#v", calls, req.Model, req.Messages)
		}
		if calls == 1 {
			return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: "1", Name: "mutate", Arguments: `{}`}}}, nil
		}
		return &model.ChatResponse{Content: "done", StopReason: model.StopReasonEnd, Usage: model.Usage{PromptTokens: 9, CompletionTokens: 2}}, nil
	}
	p.chat, follow.chat = chat, chat
	a, err := New("a", "A").WithModel(&fakeChatProvider{}).WithStorage(store).WithSystemPrompt("trimmed prompt").Build()
	if err != nil {
		t.Fatal(err)
	}
	a.Model = p // Switch after construction, before the initial session call.
	a.Hooks = hooks.Chain{sessionRecoveryHook{before: func(_ context.Context, e *hooks.Event) error {
		if e.Type == hooks.EventModelCallBefore {
			before++
			req := e.Input.(*model.ChatRequest)
			if req.Model != "initial-live" {
				t.Errorf("hook saw stale/empty model: %q", req.Model)
			}
			if before == 1 {
				req.Messages = append([]model.Message(nil), req.Messages[len(req.Messages)-1:]...)
			}
		}
		return nil
	}, after: func(_ context.Context, e *hooks.Event) error {
		if e.Type == hooks.EventModelCallAfter {
			after++
		}
		return nil
	}}}
	a.Tools.Register(&tool.Definition{Name: "mutate", Permission: tool.PermAllow, Handler: func(context.Context, map[string]any) (any, error) {
		mutations++
		a.Model = follow
		return "committed", nil
	}})
	resp, err := a.ChatWithSession(ctx, sid, "current task")
	if err != nil || resp == nil || resp.Content != "done" || resp.Usage != (model.Usage{PromptTokens: 9, CompletionTokens: 2}) {
		t.Fatalf("response=%#v err=%v", resp, err)
	}
	if calls != 2 || mutations != 1 || before != 2 || after != 2 {
		t.Fatalf("calls=%d mutations=%d hooks=%d/%d", calls, mutations, before, after)
	}
	cs := chatSessionFromEvents(store.events[sid])
	if len(cs.Messages) != 4 || cs.Messages[0].Content != "old question" || cs.Messages[2].Content != "current task" || cs.Messages[3].Content != "done" {
		t.Fatalf("request trimming changed persisted history: %#v", cs.Messages)
	}
}

func TestChatWithSession_RecoveryPreservesValidation(t *testing.T) {
	for _, mode := range []string{"input guardrail", "output guardrail", "output schema"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			p := &sessionRecoveryProvider{modelID: "model", chat: func(context.Context, *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				if calls == 1 {
					return nil, &model.APIError{StatusCode: 503}
				}
				return &model.ChatResponse{Content: "blocked", StopReason: model.StopReasonEnd}, nil
			}}
			store := newTestStorage()
			b := New("a", "A").WithModel(p).WithStorage(store)
			wantCalls := 2
			switch mode {
			case "input guardrail":
				wantCalls = 0
				b.AddInputGuardrail("input", &guardrails.BlocklistGuardrail{Blocklist: []string{"blocked"}})
			case "output guardrail":
				b.AddOutputGuardrail("output", &guardrails.BlocklistGuardrail{Blocklist: []string{"blocked"}})
			case "output schema":
				b.WithOutputSchema(map[string]any{"type": "object"})
			}
			a, err := b.Build()
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.ChatWithSession(context.Background(), "validation", "blocked")
			if err == nil || !strings.Contains(err.Error(), mode) || calls != wantCalls || len(store.events["validation"]) != 1 {
				t.Fatalf("error=%v calls=%d ledger=%d", err, calls, len(store.events["validation"]))
			}
		})
	}
}

type sessionRecoveryProvider struct {
	fakeChatProvider
	modelID string
	chat    func(context.Context, *model.ChatRequest) (*model.ChatResponse, error)
}

func (p *sessionRecoveryProvider) Model() string { return p.modelID }
func (p *sessionRecoveryProvider) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	return p.chat(ctx, req)
}

type sessionRecoveryHook struct {
	before func(context.Context, *hooks.Event) error
	after  func(context.Context, *hooks.Event) error
}

func (h sessionRecoveryHook) Before(ctx context.Context, e *hooks.Event) error {
	if h.before != nil {
		return h.before(ctx, e)
	}
	return nil
}
func (h sessionRecoveryHook) After(ctx context.Context, e *hooks.Event) error {
	if h.after != nil {
		return h.after(ctx, e)
	}
	return nil
}
