// Package model defines pluggable LLM provider interfaces.
package model

import "context"

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"` // system, user, assistant, tool
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// ChatRequest is the input to a chat completion.
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
	Tools       []any     `json:"tools,omitempty"`
}

// ChatResponse is the output of a chat completion.
type ChatResponse struct {
	ID      string    `json:"id"`
	Content string    `json:"content"`
	Role    string    `json:"role"`
	Usage   Usage     `json:"usage"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a model-requested tool invocation.
type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Provider is the interface all LLM backends must implement.
type Provider interface {
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// StreamChat returns a channel of partial responses for streaming.
	StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
}
