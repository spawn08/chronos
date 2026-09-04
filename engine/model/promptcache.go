package model

// ephemeralCache is Anthropic's default 5-minute prompt-cache checkpoint.
func ephemeralCache() map[string]any {
	return map[string]any{"type": "ephemeral"}
}

func promptCacheEnabled(req *ChatRequest) bool {
	return req != nil && !req.DisablePromptCache
}

func attachEphemeralCache(block map[string]any) {
	if block != nil {
		block["cache_control"] = ephemeralCache()
	}
}

// cacheLastContentBlock marks the last content block of msg so the next
// request can prefix-match everything up to this point. Anthropic accepts
// cache_control on text, tool_use, and tool_result blocks.
func cacheLastContentBlock(msg map[string]any) {
	switch content := msg["content"].(type) {
	case string:
		msg["content"] = []map[string]any{{
			"type":          "text",
			"text":          content,
			"cache_control": ephemeralCache(),
		}}
	case []map[string]any:
		for i := len(content) - 1; i >= 0; i-- {
			typ, _ := content[i]["type"].(string)
			if typ == "thinking" || typ == "redacted_thinking" {
				continue
			}
			attachEphemeralCache(content[i])
			return
		}
	}
}
