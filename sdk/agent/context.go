package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/engine/tool"
	"github.com/spawn08/chronos/storage"
)

type toolDefinitionsKey struct{}

// WithToolDefinitions limits the schemas advertised for one request. It copies
// definitions into the context instead of changing the agent's registry, which
// may be shared by concurrent requests.
func WithToolDefinitions(ctx context.Context, definitions []*tool.Definition) context.Context {
	return context.WithValue(ctx, toolDefinitionsKey{}, copyToolDefinitions(definitions))
}

// ToolDefinitions returns this request's selected schemas, or the supplied
// registry definitions when no request-local selection was made. Callers get
// copies so later request-local changes cannot alter the registry.
func ToolDefinitions(ctx context.Context, definitions []*tool.Definition) []*tool.Definition {
	if selected, ok := ctx.Value(toolDefinitionsKey{}).([]*tool.Definition); ok {
		return copyToolDefinitions(selected)
	}
	return copyToolDefinitions(definitions)
}

func copyToolDefinitions(definitions []*tool.Definition) []*tool.Definition {
	if len(definitions) == 0 {
		return nil
	}
	copyDefinitions := make([]*tool.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if definition == nil {
			continue
		}
		copyDefinition := *definition
		copyDefinition.Parameters = copyParameters(definition.Parameters)
		copyDefinitions = append(copyDefinitions, &copyDefinition)
	}
	return copyDefinitions
}

func copyParameters(parameters map[string]any) map[string]any {
	if parameters == nil {
		return nil
	}
	copyParameters := make(map[string]any, len(parameters))
	for key, value := range parameters {
		copyParameters[key] = copyParameterValue(value)
	}
	return copyParameters
}

func copyParameterValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return copyParameters(value)
	case []any:
		copied := make([]any, len(value))
		for i := range value {
			copied[i] = copyParameterValue(value[i])
		}
		return copied
	default:
		return value
	}
}

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

// enforceContextBudget drops the oldest conversation messages until the request
// fits within contextLimit, never touching the first protectedPrefix messages
// (the pinned/system context and any injected summary). It is the final safeguard
// of automatic compaction (WC-A-004): summarization bounds growth, and this bounds
// the absolute size of a single request so token count stays bounded.
//
// When a dropped message is an assistant turn carrying tool calls, any immediately
// following tool-result messages are dropped with it so the trimmed history never
// begins with an orphaned tool result. If the protected prefix alone already
// exceeds the limit, messages are returned unchanged past that point (there is
// nothing left to drop) — the caller is responsible for keeping pins compact.
func enforceContextBudget(counter model.TokenCounter, messages []model.Message, protectedPrefix, contextLimit int) []model.Message {
	if contextLimit <= 0 || counter == nil || protectedPrefix < 0 {
		return messages
	}
	if protectedPrefix > len(messages) {
		protectedPrefix = len(messages)
	}
	total := counter.CountTokens(messages)
	if total <= contextLimit {
		return messages
	}
	// Marginal per-message cost, relative to the empty-conversation framing, so a
	// drop subtracts one message's tokens instead of re-counting the whole slice
	// (keeps trimming O(n) rather than O(n^2)). Both counters model total tokens as
	// a per-message sum plus a constant, so this is exact.
	base := counter.CountTokens(nil)
	cost := func(m model.Message) int { return counter.CountTokens([]model.Message{m}) - base }
	original := messages
	for total > contextLimit && len(messages) > protectedPrefix+1 {
		// Remove the oldest non-protected (conversation) message.
		total -= cost(messages[protectedPrefix])
		messages = append(messages[:protectedPrefix:protectedPrefix], messages[protectedPrefix+1:]...)
		// Drop leading orphaned tool results, but never the last remaining
		// conversation message. Wiping that tail leaves a system-only request
		// which Anthropic/Gemini reject ("messages must contain at least one message").
		for len(messages) > protectedPrefix+1 && messages[protectedPrefix].Role == model.RoleTool {
			total -= cost(messages[protectedPrefix])
			messages = append(messages[:protectedPrefix:protectedPrefix], messages[protectedPrefix+1:]...)
		}
	}
	if !hasUserOrAssistant(messages, protectedPrefix) {
		messages = restoreLastUserTurn(original, protectedPrefix)
	}
	if counter.CountTokens(messages) > contextLimit {
		if collapsed := collapseToLastUser(original, protectedPrefix); len(collapsed) > 0 {
			return collapsed
		}
	}
	return messages
}

func hasUserOrAssistant(messages []model.Message, from int) bool {
	for i := from; i < len(messages); i++ {
		switch messages[i].Role {
		case model.RoleUser, model.RoleAssistant:
			return true
		}
	}
	return false
}

func restoreLastUserTurn(messages []model.Message, protectedPrefix int) []model.Message {
	if protectedPrefix < 0 {
		protectedPrefix = 0
	}
	if protectedPrefix > len(messages) {
		protectedPrefix = len(messages)
	}
	lastUser := -1
	for i := len(messages) - 1; i >= protectedPrefix; i-- {
		if messages[i].Role == model.RoleUser {
			lastUser = i
			break
		}
	}
	out := make([]model.Message, 0, len(messages))
	out = append(out, messages[:protectedPrefix]...)
	if lastUser >= 0 {
		return append(out, messages[lastUser:]...)
	}
	if protectedPrefix < len(messages) {
		return append(out, messages[len(messages)-1])
	}
	return out
}

func collapseToLastUser(messages []model.Message, protectedPrefix int) []model.Message {
	if protectedPrefix < 0 {
		protectedPrefix = 0
	}
	if protectedPrefix > len(messages) {
		protectedPrefix = len(messages)
	}
	lastUser := -1
	for i := len(messages) - 1; i >= protectedPrefix; i-- {
		if messages[i].Role == model.RoleUser {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return nil
	}
	out := make([]model.Message, 0, protectedPrefix+1)
	out = append(out, messages[:protectedPrefix]...)
	return append(out, messages[lastUser])
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
