package model

import "strings"

// ephemeralCache is Anthropic's default 5-minute prompt-cache checkpoint.
func ephemeralCache() map[string]any {
	return map[string]any{"type": "ephemeral"}
}

// prefixCache is the checkpoint for the static prompt prefix. ttl "1h"
// requests Anthropic's one-hour cache; any other value keeps the default.
// A one-hour entry must precede every five-minute entry in the request,
// which holds because tools and system render before messages.
func prefixCache(ttl string) map[string]any {
	if ttl == "1h" {
		return map[string]any{"type": "ephemeral", "ttl": "1h"}
	}
	return ephemeralCache()
}

func promptCacheEnabled(req *ChatRequest) bool {
	return req != nil && !req.DisablePromptCache
}

// tailCache is the checkpoint on the last message. A one-hour tail is valid
// only after a one-hour prefix: Anthropic rejects a one-hour entry that
// follows a five-minute one, so a five-minute prefix keeps the tail default.
func tailCache(prefixTTL, tailTTL string) map[string]any {
	if prefixTTL == "1h" && tailTTL == "1h" {
		return prefixCache("1h")
	}
	return ephemeralCache()
}

// cacheLastContentBlock marks the last content block of msg with checkpoint
// so the next request can prefix-match everything up to this point. Anthropic
// accepts cache_control on text, tool_use, and tool_result blocks.
func cacheLastContentBlock(msg map[string]any, checkpoint map[string]any) {
	switch content := msg["content"].(type) {
	case string:
		msg["content"] = []map[string]any{{
			"type":          "text",
			"text":          content,
			"cache_control": checkpoint,
		}}
	case []map[string]any:
		for i := len(content) - 1; i >= 0; i-- {
			typ, _ := content[i]["type"].(string)
			if typ == "thinking" || typ == "redacted_thinking" {
				continue
			}
			content[i]["cache_control"] = checkpoint
			return
		}
	}
}

// appendUncachedTail adds per-call context (Message.Uncached) after the last
// cache breakpoint, as trailing text of the final user message: the cached
// prefix ends before it, so the next request, whose tail differs, still
// matches that prefix. A user message is added when the last one is not.
func appendUncachedTail(body map[string]any, tail []string) {
	if len(tail) == 0 {
		return
	}
	block := map[string]any{"type": "text", "text": strings.Join(tail, "\n\n")}
	msgs, _ := body["messages"].([]map[string]any)
	if n := len(msgs); n > 0 && msgs[n-1]["role"] == RoleUser {
		switch content := msgs[n-1]["content"].(type) {
		case string:
			msgs[n-1]["content"] = []map[string]any{{"type": "text", "text": content}, block}
			return
		case []map[string]any:
			msgs[n-1]["content"] = append(content, block)
			return
		}
	}
	body["messages"] = append(msgs, map[string]any{"role": RoleUser, "content": []map[string]any{block}})
}
