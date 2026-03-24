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

// ContentPart represents a multi-modal content part within a message.
type ContentPart struct {
	Type     string `json:"type"`               // "text", "image_url", "file"
	Text     string `json:"text,omitempty"`      // for type "text"
	ImageURL string `json:"image_url,omitempty"` // for type "image_url" — URL or base64 data URI
	MimeType string `json:"mime_type,omitempty"` // MIME type for image or file
	FileName string `json:"file_name,omitempty"` // original filename for attachments
	Data     []byte `json:"-"`                   // raw binary data (not serialized to JSON)
}

// Message represents a chat message.
type Message struct {
	Role       string        `json:"role"` // system, user, assistant, tool
	Content    string        `json:"content"`
	Parts      []ContentPart `json:"parts,omitempty"` // multi-modal content parts
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty"`
}

// AddImageURL adds an image URL content part to the message.
func (m *Message) AddImageURL(url, mimeType string) {
	m.Parts = append(m.Parts, ContentPart{
		Type:     "image_url",
		ImageURL: url,
		MimeType: mimeType,
	})
}

// AddFile adds a file attachment content part to the message.
func (m *Message) AddFile(filename, mimeType string, data []byte) {
	m.Parts = append(m.Parts, ContentPart{
		Type:     "file",
		FileName: filename,
		MimeType: mimeType,
		Data:     data,
	})
}

// ToolCall represents a model-requested tool invocation.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolDefinition describes a tool the model can call (OpenAI function-calling format).
type ToolDefinition struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
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
	// ResponseFormat controls output format. Values:
	//   "json_object"  — model returns valid JSON (no schema enforcement)
	//   "json_schema"  — model returns JSON conforming to Metadata["json_schema"]
	ResponseFormat string         `json:"response_format,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
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
	APIKey        string `json:"api_key"`
	BaseURL       string `json:"base_url,omitempty"`
	Model         string `json:"model"`
	MaxRetries    int    `json:"max_retries,omitempty"`
	TimeoutSec    int    `json:"timeout_sec,omitempty"`
	OrgID         string `json:"org_id,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"` // override default context window size for the model
}
