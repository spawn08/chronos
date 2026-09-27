package agent

import (
	"context"
	"time"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/stream"
)

// toolInputInterval throttles EventToolInput: arguments arrive as many small
// fragments and consumers only need the running size.
const toolInputInterval = 250 * time.Millisecond

// toolInputProgress publishes EventToolInput while a streamed tool call's
// arguments are being generated, so a long call (for example a large file
// write) is visible before it completes. Providers mark the start of a call
// with its ID; later fragments carry only argument text.
type toolInputProgress struct {
	agent     *Agent
	now       func() time.Time
	id        string
	name      string
	bytes     int
	published time.Time
}

func (p *toolInputProgress) observe(ctx context.Context, calls []model.ToolCall) {
	for _, tc := range calls {
		if tc.ID != "" && tc.ID != p.id {
			p.id, p.name, p.bytes = tc.ID, tc.Name, len(tc.Arguments)
			p.publish(ctx)
			continue
		}
		if p.id == "" {
			continue
		}
		p.bytes += len(tc.Arguments)
		if p.now().Sub(p.published) >= toolInputInterval {
			p.publish(ctx)
		}
	}
}

func (p *toolInputProgress) publish(ctx context.Context) {
	p.published = p.now()
	p.agent.publish(ctx, stream.Event{Type: stream.EventToolInput, Data: map[string]any{
		"agent": p.agent.ID, "id": p.id, "tool": p.name, "bytes": p.bytes,
	}})
}
