package agent

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

func TestProviderContinuationPersistsAcrossSessionAndSummaryReplay(t *testing.T) {
	for _, test := range []struct {
		name  string
		state any
	}{
		{"anthropic thinking", []map[string]any{{"type": "thinking", "thinking": "reason", "signature": "sig"}}},
		{"responses items", []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"r-1"}`)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStorage()
			message := model.Message{Role: model.RoleAssistant, Content: "answer", ProviderState: test.state}
			if err := persistMessage(context.Background(), store, "session", 1, message); err != nil {
				t.Fatal(err)
			}
			plain := jsonRoundtripSessionEvents(t, store.events["session"])
			replayed := chatSessionFromEvents(plain)
			if replayed.recoveryErr != nil || len(replayed.Messages) != 1 || !sameProviderState(t, replayed.Messages[0].ProviderState, test.state) {
				t.Fatalf("replayed provider state = %#v, error = %v", replayed.Messages, replayed.recoveryErr)
			}
			if err := persistSummaryCheckpoint(context.Background(), store, "session", 2, 1, model.SummarizationResult{Summary: "summary", PreservedMessages: []model.Message{message}}); err != nil {
				t.Fatal(err)
			}
			replayed = chatSessionFromEvents(jsonRoundtripSessionEvents(t, store.events["session"]))
			if replayed.recoveryErr != nil || len(replayed.Messages) != 1 || !sameProviderState(t, replayed.Messages[0].ProviderState, test.state) {
				t.Fatalf("summary provider state = %#v, error = %v", replayed.Messages, replayed.recoveryErr)
			}
		})
	}
}

func sameProviderState(t *testing.T, actual, expected any) bool {
	t.Helper()
	var left, right any
	for i, value := range []any{actual, expected} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if err := json.Unmarshal(encoded, &left); err != nil {
				t.Fatal(err)
			}
		} else if err := json.Unmarshal(encoded, &right); err != nil {
			t.Fatal(err)
		}
	}
	return reflect.DeepEqual(left, right)
}

func TestProviderContinuationRejectsUnsupportedOrCorruptState(t *testing.T) {
	store := newTestStorage()
	if err := persistMessage(context.Background(), store, "session", 1, model.Message{Role: model.RoleAssistant, ProviderState: struct{ Secret string }{Secret: "unsupported"}}); err == nil {
		t.Fatal("unsupported provider state was silently dropped")
	}
	events := []*storage.Event{{Type: "chat_message", SeqNum: 1, Payload: map[string]any{"role": "assistant", "content": "", "provider_state": map[string]any{"kind": "unknown", "data": []any{}}}}}
	if got := chatSessionFromEvents(events); got.recoveryErr == nil {
		t.Fatal("corrupt provider continuation was accepted")
	}
}

func jsonRoundtripSessionEvents(t *testing.T, events []*storage.Event) []*storage.Event {
	t.Helper()
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []*storage.Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
