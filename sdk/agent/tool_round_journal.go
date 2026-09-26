package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
)

// ToolRoundJournal is installed by a durable worker. A checkpoint contains a
// complete assistant tool reply and all of its results, never a partial round.
type ToolRoundJournal interface {
	ResumeToolRound(context.Context, string, string, string) ([]model.Message, error)
	CheckpointToolRound(context.Context, string, string, string, int, []model.Message) error
}

// AgentReplyJournal is optional for durable workers that can account for a
// completed agent reply. A reply is replayed without admitting another model
// call; incomplete/unknown provider outcomes have no checkpoint.
type AgentReplyJournal interface {
	ResumeAgentReply(context.Context, string, string, string) (*model.ChatResponse, error)
	CheckpointAgentReply(context.Context, string, string, string, *model.ChatResponse) error
}

func agentReplyJournalFromContext(ctx context.Context) AgentReplyJournal {
	journal, _ := toolRoundJournalFromContext(ctx).(AgentReplyJournal)
	return journal
}

type toolRoundJournalKey struct{}
type toolRoundInputKey struct{}

func WithToolRoundJournal(ctx context.Context, journal ToolRoundJournal) context.Context {
	if journal == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRoundJournalKey{}, journal)
}

func toolRoundJournalFromContext(ctx context.Context) ToolRoundJournal {
	journal, _ := ctx.Value(toolRoundJournalKey{}).(ToolRoundJournal)
	return journal
}

type storedRoundMessage struct {
	Message       model.Message        `json:"message"`
	ProviderState *storedProviderState `json:"provider_state,omitempty"`
}

// EncodeToolRound preserves provider-owned continuation types through JSON.
func EncodeToolRound(messages []model.Message) ([]byte, error) {
	stored := make([]storedRoundMessage, len(messages))
	for i, msg := range messages {
		state, err := encodeProviderState(msg.ProviderState)
		if err != nil {
			return nil, err
		}
		msg.ProviderState = nil
		stored[i] = storedRoundMessage{Message: msg, ProviderState: state}
	}
	return json.Marshal(stored)
}

func DecodeToolRound(data []byte) ([]model.Message, error) {
	var stored []storedRoundMessage
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("decode tool round: %w", err)
	}
	messages := make([]model.Message, len(stored))
	for i, msg := range stored {
		messages[i] = msg.Message
		if msg.ProviderState != nil {
			state, err := decodeProviderState(msg.ProviderState)
			if err != nil {
				return nil, err
			}
			messages[i].ProviderState = state
		}
	}
	return messages, nil
}
