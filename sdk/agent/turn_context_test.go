package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/spawn08/chronos/engine/model"
)

// turnPins returns a static pin and a turn-scoped pin that differs on every
// evaluation, like memory recalled for the current message.
func turnPins() func(context.Context) []model.Message {
	var mu sync.Mutex
	n := 0
	return func(context.Context) []model.Message {
		mu.Lock()
		defer mu.Unlock()
		n++
		return []model.Message{
			{Role: model.RoleSystem, Content: "static pin"},
			{Role: model.RoleSystem, Content: "recalled " + strings.Repeat("!", n), TurnScoped: true},
		}
	}
}

func systemAndUsers(req *model.ChatRequest) (system, users []string) {
	for _, m := range req.Messages {
		switch m.Role {
		case model.RoleSystem:
			system = append(system, m.Content)
		case model.RoleUser:
			users = append(users, m.Content)
		}
	}
	return system, users
}

// TestSessionTurnContextBecomesHistory: turn-scoped pins leave the system
// context and are folded into their turn's user message, so every later
// request repeats earlier turns byte for byte and the system prefix is stable.
func TestSessionTurnContextBecomesHistory(t *testing.T) {
	prov := &streamProvider{scripts: [][]*model.ChatResponse{
		{{Role: model.RoleAssistant, Content: "one", Delta: true}, {Role: model.RoleAssistant, StopReason: model.StopReasonEnd}},
		{{Role: model.RoleAssistant, Content: "two", Delta: true}, {Role: model.RoleAssistant, StopReason: model.StopReasonEnd}},
	}}
	store := newTestStorage()
	a, _ := New("a1", "Test").WithModel(prov).WithStorage(store).Build()
	a.ContextPinsFn = turnPins()

	for _, msg := range []string{"first", "second"} {
		stream, err := a.ChatStreamWithSession(context.Background(), "s1", msg)
		if err != nil {
			t.Fatalf("ChatStreamWithSession(%q): %v", msg, err)
		}
		if _, _, err := collectStream(t, stream); err != nil {
			t.Fatalf("stream %q: %v", msg, err)
		}
	}

	sys1, users1 := systemAndUsers(prov.requests[0])
	sys2, users2 := systemAndUsers(prov.requests[1])
	if strings.Join(sys1, "|") != strings.Join(sys2, "|") || strings.Contains(strings.Join(sys2, "|"), "recalled") {
		t.Fatalf("system context = %q then %q, want the same static pins only", sys1, sys2)
	}
	wantFirst := "<turn_context>\nrecalled !\n</turn_context>\n\nfirst"
	if len(users1) != 1 || users1[0] != wantFirst {
		t.Fatalf("first request user messages = %q, want %q", users1, wantFirst)
	}
	if len(users2) != 2 || users2[0] != wantFirst || users2[1] != "<turn_context>\nrecalled !!\n</turn_context>\n\nsecond" {
		t.Fatalf("second request user messages = %q, want the first turn unchanged then the second with its context", users2)
	}
	if payload, _ := store.events["s1"][0].Payload.(map[string]any); payload["content"] != wantFirst {
		t.Fatalf("persisted user message = %v, want it to carry its turn context", payload["content"])
	}
}

func TestSplitTurnContextWithoutTurnPinsKeepsMessage(t *testing.T) {
	kept, msg := splitTurnContext([]model.Message{{Role: model.RoleSystem, Content: "s"}, {Role: model.RoleSystem, Content: "  ", TurnScoped: true}}, "hi")
	if len(kept) != 1 || msg != "hi" {
		t.Fatalf("splitTurnContext = %v, %q", kept, msg)
	}
}
