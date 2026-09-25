package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/spawn08/chronos/engine/hooks"
	"github.com/spawn08/chronos/storage"
)

type summaryAccountingHook struct {
	before     int
	after      int
	afterError error
}

func (h *summaryAccountingHook) Before(_ context.Context, event *hooks.Event) error {
	if event.Type == hooks.EventModelCallBefore {
		h.before++
	}
	return nil
}

func (h *summaryAccountingHook) After(_ context.Context, event *hooks.Event) error {
	if event.Type == hooks.EventModelCallAfter {
		h.after++
		return h.afterError
	}
	return nil
}

func TestCompactSessionAccountsForSummaryAndPropagatesReceiptFailure(t *testing.T) {
	for _, test := range []struct {
		name           string
		afterError     error
		wantCheckpoint bool
	}{
		{name: "observed summary", wantCheckpoint: true},
		{name: "lost accounting receipt", afterError: errors.New("usage receipt unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStorage()
			sessionID := "compaction"
			store.sessions[sessionID] = &storage.Session{ID: sessionID, AgentID: "worker", Status: "active"}
			store.events[sessionID] = []*storage.Event{
				{ID: "e1", SessionID: sessionID, SeqNum: 1, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "investigate"}},
				{ID: "e2", SessionID: sessionID, SeqNum: 2, Type: "chat_message", Payload: map[string]any{"role": "assistant", "content": "found"}},
				{ID: "e3", SessionID: sessionID, SeqNum: 3, Type: "chat_message", Payload: map[string]any{"role": "user", "content": "continue"}},
			}
			hook := &summaryAccountingHook{afterError: test.afterError}
			a, err := New("worker", "Worker").WithModel(&fakeChatProvider{content: "summary"}).WithStorage(store).
				WithContextConfig(ContextConfig{PreserveRecentTurns: 1}).AddHook(hook).Build()
			if err != nil {
				t.Fatal(err)
			}
			err = a.CompactSession(context.Background(), sessionID)
			if test.afterError != nil && !errors.Is(err, test.afterError) || test.afterError == nil && err != nil {
				t.Fatalf("CompactSession() error = %v", err)
			}
			if hook.before != 1 || hook.after != 1 {
				t.Fatalf("model accounting hooks = before %d after %d", hook.before, hook.after)
			}
			last := store.events[sessionID][len(store.events[sessionID])-1]
			if got := last.Type == "chat_summary"; got != test.wantCheckpoint {
				t.Fatalf("summary checkpoint persisted = %v, want %v", got, test.wantCheckpoint)
			}
		})
	}
}
