package agent

import (
	"encoding/json"
	"fmt"
)

type storedProviderState struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// Provider-owned continuation data is serialized with an explicit kind;
// otherwise a JSON round trip changes the types required by provider adapters.
func encodeProviderState(state any) (*storedProviderState, error) {
	if state == nil {
		return nil, nil
	}
	result := &storedProviderState{}
	switch state.(type) {
	case []json.RawMessage:
		result.Kind = "responses_raw_items_v1"
	case []map[string]any, []any:
		result.Kind = "assistant_blocks_v1"
	default:
		return nil, fmt.Errorf("unsupported provider continuation type %T", state)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode provider continuation: %w", err)
	}
	result.Data = data
	return result, nil
}

func decodeProviderState(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("read provider continuation: %w", err)
	}
	var record storedProviderState
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, fmt.Errorf("decode provider continuation: %w", err)
	}
	switch record.Kind {
	case "responses_raw_items_v1":
		var items []json.RawMessage
		if err := json.Unmarshal(record.Data, &items); err != nil {
			return nil, fmt.Errorf("decode Responses continuation: %w", err)
		}
		return items, nil
	case "assistant_blocks_v1":
		var blocks []map[string]any
		if err := json.Unmarshal(record.Data, &blocks); err != nil {
			return nil, fmt.Errorf("decode assistant blocks: %w", err)
		}
		return blocks, nil
	default:
		return nil, fmt.Errorf("unknown provider continuation kind %q", record.Kind)
	}
}
