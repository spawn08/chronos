package evals

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spawn08/chronos/storage"
)

// Dataset is a curated set of input/expected pairs used to evaluate an agent or
// graph. It is the "golden" corpus at the center of the eval-driven loop: capture
// real runs into a Dataset, then run it against a target and gate on regressions.
type Dataset struct {
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"created_at"`
	Cases     []DatasetCase `json:"cases"`
}

// DatasetCase is a single evaluation example: an input and the expected (golden)
// output, plus optional provenance metadata (e.g. the source session id).
type DatasetCase struct {
	Input    string         `json:"input"`
	Expected string         `json:"expected"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MarshalDataset renders a dataset as indented JSON for on-disk storage.
func MarshalDataset(d *Dataset) ([]byte, error) {
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal dataset: %w", err)
	}
	return data, nil
}

// LoadDataset parses a dataset from its JSON representation.
func LoadDataset(data []byte) (*Dataset, error) {
	var d Dataset
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("load dataset: %w", err)
	}
	return &d, nil
}

// CaptureFromSession builds a Dataset from a session's stored conversation: each
// user turn becomes an input and the assistant reply that follows it becomes the
// expected output. It reads the append-only event ledger (storage.Storage), so it
// captures exactly what really happened. The context tenant scopes the read, so a
// caller only ever captures its own session. The returned dataset may be empty if
// the session has no complete user→assistant pair.
func CaptureFromSession(ctx context.Context, store storage.Storage, sessionID, name string) (*Dataset, error) {
	if store == nil {
		return nil, fmt.Errorf("capture dataset: nil storage")
	}
	events, err := store.ListEvents(ctx, sessionID, 0)
	if err != nil {
		return nil, fmt.Errorf("capture dataset: list events: %w", err)
	}

	ds := &Dataset{Name: name, CreatedAt: time.Now()}
	var pendingInput string
	haveInput := false
	for _, evt := range events {
		if evt.Type != "chat_message" {
			continue
		}
		payload, ok := evt.Payload.(map[string]any)
		if !ok {
			continue
		}
		role, _ := payload["role"].(string)
		content, _ := payload["content"].(string)
		switch role {
		case "user":
			pendingInput, haveInput = content, true
		case "assistant":
			// Pair the assistant reply with the most recent user turn. Skip empty
			// replies (e.g. tool-call-only turns carry no golden text).
			if haveInput && content != "" {
				ds.Cases = append(ds.Cases, DatasetCase{
					Input:    pendingInput,
					Expected: content,
					Metadata: map[string]any{"session_id": sessionID},
				})
				haveInput = false
			}
		}
	}
	return ds, nil
}
