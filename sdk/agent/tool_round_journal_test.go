package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/sdk/agent"
	memorystore "github.com/spawn08/chronos/storage/adapters/memory"
)

type roundJournalFixture struct {
	saved []model.Message
	ready bool
}

func (j *roundJournalFixture) ResumeToolRound(_ context.Context, _, _, _ string) ([]model.Message, error) {
	if j.ready {
		return j.saved, nil
	}
	return nil, nil
}

func (j *roundJournalFixture) CheckpointToolRound(_ context.Context, _, _, _ string, _ int, messages []model.Message) error {
	encoded, err := agent.EncodeToolRound(messages)
	if err != nil {
		return err
	}
	j.saved, err = agent.DecodeToolRound(encoded)
	return err
}

type interruptRoundFixture struct{}

func (interruptRoundFixture) AfterToolRound(context.Context, agent.ToolRound) (agent.ToolLoopAction, error) {
	return agent.ToolLoopAction{}, errors.New("injected crash after checkpoint")
}

func TestToolRoundJournalResumesWithoutRepeatingReplyOrTool(t *testing.T) {
	for _, session := range []bool{false, true} {
		t.Run(map[bool]string{true: "session", false: "chat"}[session], func(t *testing.T) {
			journal := &roundJournalFixture{}
			store := memorystore.New()
			calls, effects := 0, 0
			provider := &runtimeProvider{id: "fixture", chat: func(_ context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
				calls++
				if calls == 1 {
					return &model.ChatResponse{StopReason: model.StopReasonToolCall, ToolCalls: []model.ToolCall{{ID: "effect", Name: "write", Arguments: `{}`}}, ProviderState: []json.RawMessage{json.RawMessage(`{"id":"prior"}`)}}, nil
				}
				if len(req.Messages) < 3 || req.Messages[len(req.Messages)-1].Content != `"written"` || req.Messages[len(req.Messages)-2].ToolCalls[0].ID != "effect" {
					t.Errorf("resume request lost completed round: %+v", req.Messages)
				}
				if _, ok := req.Messages[len(req.Messages)-2].ProviderState.([]json.RawMessage); !ok {
					t.Errorf("provider continuation type = %T", req.Messages[len(req.Messages)-2].ProviderState)
				}
				return &model.ChatResponse{Content: "done", StopReason: model.StopReasonEnd}, nil
			}}
			newAgent := func() *agent.Agent {
				a, err := agent.New("worker", "Worker").WithModel(provider).WithStorage(store).
					WithContextConfig(agent.ContextConfig{PersistToolRounds: true}).
					AddTool(&tool.Definition{Name: "write", Permission: tool.PermAllow, Handler: func(context.Context, map[string]any) (any, error) {
						effects++
						return "written", nil
					}}).Build()
				if err != nil {
					t.Fatal(err)
				}
				return a
			}
			run := func(ctx context.Context, a *agent.Agent) (*model.ChatResponse, error) {
				if session {
					return a.ChatWithSession(ctx, "fixture-session", "task")
				}
				return a.Chat(ctx, "task")
			}
			ctx := agent.WithToolRoundJournal(context.Background(), journal)
			first := agent.WithToolLoopController(ctx, "worker", interruptRoundFixture{})
			if _, err := run(first, newAgent()); err == nil || len(journal.saved) == 0 {
				t.Fatalf("first attempt error=%v checkpoint=%+v", err, journal.saved)
			}
			journal.ready = true
			response, err := run(ctx, newAgent())
			if err != nil || response.Content != "done" || calls != 2 || effects != 1 {
				t.Fatalf("recovered reply=%+v error=%v provider=%d effects=%d", response, err, calls, effects)
			}
		})
	}
}
