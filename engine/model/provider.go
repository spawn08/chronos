// Package model defines pluggable LLM provider interfaces.
package model

import "context"

// Role constants for chat messages.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// StopReason indicates why the model stopped generating.
type StopReason string

const (
	StopReasonEnd       StopReason = "end"
	StopReasonMaxTokens StopReason = "max_tokens"
	StopReasonToolCall  StopReason = "tool_call"
	StopReasonFilter    StopReason = "content_filter"
)

// Message represents a chat message.
type Message struct {
	Role       string     `json:"role"` // system, user, assistant, tool
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall represents a model-requested tool invocation.
type ToolCall struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition describes a tool the model can call (OpenAI function-calling format).
type ToolDefinition struct {
	Type     string       `json:"type"` // "function"
	Function FunctionDef  `json:"function"`
}

// FunctionDef describes a callable function for the model.
type FunctionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON Schema
}

// ChatRequest is the input to a chat completion.
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	TopP        float64          `json:"top_p,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Stop        []string         `json:"stop,omitempty"`
	// ResponseFormat optionally forces JSON output. Set to "json_object" for JSON mode.
	ResponseFormat string `json:"response_format,omitempty"`
}

// ChatResponse is the output of a chat completion.
type ChatResponse struct {
	ID         string     `json:"id"`
	Content    string     `json:"content"`
	Role       string     `json:"role"`
	Usage      Usage      `json:"usage"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	StopReason StopReason `json:"stop_reason,omitempty"`
	// Delta is true when this is a partial streaming response.
	Delta bool `json:"delta,omitempty"`
}

// Usage tracks token consumption.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Provider is the interface all LLM backends must implement.
type Provider interface {
	// Chat sends a request and returns a complete response.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// StreamChat returns a channel of partial responses for streaming.
	// The channel is closed when the response is complete.
	StreamChat(ctx context.Context, req *ChatRequest) (<-chan *ChatResponse, error)
	// Name returns a human-readable name for this provider.
	Name() string
	// Model returns the default model ID for this provider.
	Model() string
}

// ProviderConfig holds common configuration shared by all providers.
type ProviderConfig struct {
	APIKey     string  `json:"api_key"`
	BaseURL    string  `json:"base_url,omitempty"`
	Model      string  `json:"model"`
	MaxRetries int     `json:"max_retries,omitempty"`
	TimeoutSec int     `json:"timeout_sec,omitempty"`
	OrgID      string  `json:"org_id,omitempty"`
}
