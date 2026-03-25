package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/storage"
)

// EvictionResult holds the outcome of evicting a large tool result.
type EvictionResult struct {
	StorageKey string `json:"storage_key"`
	Preview    string `json:"preview"`
	FullSize   int    `json:"full_size"`
}

// EvictLargeResult stores a large tool result in storage and returns a
// truncated preview with a reference key. The agent can re-read the full
// result using the read_stored_result built-in tool.
func EvictLargeResult(ctx context.Context, store storage.Storage, sessionID, toolName string, result any) (*EvictionResult, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal result: %w", err)
	}

	if len(data) < 1000 {
		return nil, nil
	}

	hash := sha256.Sum256(data)
	key := fmt.Sprintf("tool_result_%s_%x", toolName, hash[:8])

	mem := &storage.MemoryRecord{
		ID:      key,
		AgentID: sessionID,
		Kind:    "tool_result_evicted",
		Key:     key,
		Value:   string(data),
	}
	if err := store.PutMemory(ctx, mem); err != nil {
		return nil, fmt.Errorf("store evicted result: %w", err)
	}

	previewLen := 500
	if previewLen > len(data) {
		previewLen = len(data)
	}

	return &EvictionResult{
		StorageKey: key,
		Preview:    string(data[:previewLen]) + "... [truncated, use read_stored_result tool with key=" + key + "]",
		FullSize:   len(data),
	}, nil
}

// ReadStoredResult retrieves a previously evicted tool result from storage.
func ReadStoredResult(ctx context.Context, store storage.Storage, sessionID, key string) (string, error) {
	mem, err := store.GetMemory(ctx, sessionID, key)
	if err != nil {
		return "", fmt.Errorf("read stored result: %w", err)
	}
	val, ok := mem.Value.(string)
	if !ok {
		data, _ := json.Marshal(mem.Value)
		return string(data), nil
	}
	return val, nil
}

// CompressToolCalls removes older tool call/result pairs from message history,
// keeping only the most recent maxCalls pairs. System messages and non-tool
// messages are always preserved.
func CompressToolCalls(messages []model.Message, maxCalls int) []model.Message {
	if maxCalls <= 0 || len(messages) == 0 {
		return messages
	}

	type indexedMsg struct {
		idx int
		msg model.Message
	}

	var toolMsgs []indexedMsg
	var otherMsgs []indexedMsg

	for i := range messages {
		if messages[i].Role == model.RoleTool || (messages[i].Role == model.RoleAssistant && len(messages[i].ToolCalls) > 0) {
			toolMsgs = append(toolMsgs, indexedMsg{idx: i, msg: messages[i]})
		} else {
			otherMsgs = append(otherMsgs, indexedMsg{idx: i, msg: messages[i]})
		}
	}

	if len(toolMsgs) <= maxCalls*2 {
		return messages
	}

	keepFrom := len(toolMsgs) - maxCalls*2
	keepSet := make(map[int]bool)
	for i := range otherMsgs {
		keepSet[otherMsgs[i].idx] = true
	}
	for i := keepFrom; i < len(toolMsgs); i++ {
		keepSet[toolMsgs[i].idx] = true
	}

	var result []model.Message
	for i := range messages {
		if keepSet[i] {
			result = append(result, messages[i])
		}
	}
	return result
}
