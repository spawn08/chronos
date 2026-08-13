package model

import (
	"context"
	"strings"
)

// maxSSELineBytes bounds a single SSE line. LLM tool-call arguments can produce
// very long single-line JSON payloads that exceed bufio.Scanner's default 64KB
// cap; without this override those lines are silently truncated.
const maxSSELineBytes = 1 << 20 // 1 MiB

// sendCtx delivers cr on ch unless ctx is done, in which case it returns false
// so the producing goroutine can unwind instead of blocking forever on an
// abandoned stream (preventing goroutine/connection leaks on client disconnect).
func sendCtx(ctx context.Context, ch chan<- *ChatResponse, cr *ChatResponse) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- cr:
		return true
	}
}

// AggregateStream consumes a StreamChat delta channel and merges the partial
// responses into a single complete ChatResponse: it concatenates text content,
// reassembles streamed tool calls, accumulates token usage and captures the
// final stop reason. It is the recommended way for non-streaming callers (and
// SDK-level orchestration) to obtain a full response from Provider.StreamChat.
//
// If ctx is canceled it returns the context error. If any delta carried a
// streaming error (ChatResponse.Err) that error is returned once the stream
// drains.
func AggregateStream(ctx context.Context, ch <-chan *ChatResponse) (*ChatResponse, error) {
	final := &ChatResponse{Role: RoleAssistant}
	var content strings.Builder
	var reasoning strings.Builder
	// Tool calls are keyed by their streamed identity so fragmented argument
	// deltas reassemble in order.
	toolOrder := make([]string, 0)
	toolByKey := make(map[string]*ToolCall)
	var streamErr error

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case cr, ok := <-ch:
			if !ok {
				final.Content = content.String()
				final.Reasoning = reasoning.String()
				for _, k := range toolOrder {
					final.ToolCalls = append(final.ToolCalls, *toolByKey[k])
				}
				if len(final.ToolCalls) > 0 && final.StopReason == "" {
					final.StopReason = StopReasonToolCall
				}
				if final.StopReason == "" {
					final.StopReason = StopReasonEnd
				}
				return final, streamErr
			}
			if cr == nil {
				continue
			}
			if cr.Err != nil {
				streamErr = cr.Err
			}
			if cr.ID != "" {
				final.ID = cr.ID
			}
			content.WriteString(cr.Content)
			reasoning.WriteString(cr.Reasoning)
			if cr.Usage.PromptTokens > 0 {
				final.Usage.PromptTokens = cr.Usage.PromptTokens
			}
			if cr.Usage.CompletionTokens > 0 {
				final.Usage.CompletionTokens = cr.Usage.CompletionTokens
			}
			if cr.StopReason != "" {
				final.StopReason = cr.StopReason
			}
			mergeToolCalls(cr.ToolCalls, &toolOrder, toolByKey)
		}
	}
}

// mergeToolCalls folds streamed tool-call fragments into the running set. A
// fragment with a fresh ID/Name starts a new call; subsequent fragments append
// their argument text to the most recent call when they carry no new identity.
func mergeToolCalls(fragments []ToolCall, order *[]string, byKey map[string]*ToolCall) {
	for _, tc := range fragments {
		key := tc.ID
		if key == "" && tc.Name != "" {
			key = tc.Name
		}
		if key == "" {
			// Argument-only fragment: append to the most recent tool call.
			if len(*order) > 0 {
				last := byKey[(*order)[len(*order)-1]]
				last.Arguments += tc.Arguments
			}
			continue
		}
		existing, ok := byKey[key]
		if !ok {
			cp := tc
			byKey[key] = &cp
			*order = append(*order, key)
			continue
		}
		if existing.Name == "" && tc.Name != "" {
			existing.Name = tc.Name
		}
		if existing.ID == "" && tc.ID != "" {
			existing.ID = tc.ID
		}
		existing.Arguments += tc.Arguments
	}
}
