package agent

import (
	"context"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
)

// Summarization is a billable model call. Route it through the same hook
// boundary as chat/tool rounds so reservations, usage and after-hook failures
// cannot disappear during compaction.
type summarizerProvider struct {
	agent    *Agent
	provider model.Provider
}

func (p summarizerProvider) Name() string  { return p.provider.Name() }
func (p summarizerProvider) Model() string { return p.provider.Model() }

func (p summarizerProvider) Chat(ctx context.Context, request *model.ChatRequest) (*model.ChatResponse, error) {
	return p.agent.modelCall(ctx, p.provider, request, false, func() (*model.ChatResponse, error) {
		return p.provider.Chat(ctx, request)
	}, nil)
}

func (p summarizerProvider) StreamChat(context.Context, *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, fmt.Errorf("summarizer streaming is unavailable")
}
