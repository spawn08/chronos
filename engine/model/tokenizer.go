package model

import (
	"fmt"

	"github.com/tiktoken-go/tokenizer"
)

// TokenCounter estimates the token count for a set of messages.
type TokenCounter interface {
	CountTokens(messages []Message) int
	CountString(s string) int
}

// perMessageOverhead approximates the fixed per-message framing tokens (role
// markers and separators) that chat APIs add around each message.
const perMessageOverhead = 4

// conversationOverhead approximates the fixed framing tokens for a whole
// conversation (e.g. the priming assistant turn).
const conversationOverhead = 3

// BPECounter counts tokens using a real byte-pair-encoding (BPE) tokenizer
// (tiktoken encodings), giving exact counts for OpenAI models and a close
// approximation for other providers. It replaces the character-ratio heuristic
// for accuracy in budgeting and context-window management.
type BPECounter struct {
	codec    tokenizer.Codec
	encoding tokenizer.Encoding
	// fallback is used when the BPE codec returns an error for a string.
	fallback *EstimatingCounter
}

// NewBPECounter builds a BPE token counter for the given model. Known OpenAI
// models map to their exact encoding; unknown models (including Anthropic,
// Gemini, etc.) fall back to the newest general-purpose encoding (o200k_base),
// which yields a close approximation.
func NewBPECounter(modelName string) (*BPECounter, error) {
	if codec, err := tokenizer.ForModel(tokenizer.Model(modelName)); err == nil {
		return &BPECounter{codec: codec, encoding: tokenizer.O200kBase, fallback: NewEstimatingCounter()}, nil
	}
	codec, err := tokenizer.Get(tokenizer.O200kBase)
	if err != nil {
		return nil, fmt.Errorf("load bpe encoding: %w", err)
	}
	return &BPECounter{codec: codec, encoding: tokenizer.O200kBase, fallback: NewEstimatingCounter()}, nil
}

// NewTokenCounter returns the best available token counter for a model: a real
// BPE counter when its encoding can be loaded, otherwise the character-ratio
// estimator. It never returns an error, so callers can use it inline.
func NewTokenCounter(modelName string) TokenCounter {
	if c, err := NewBPECounter(modelName); err == nil {
		return c
	}
	return NewEstimatingCounter()
}

// CountString returns the exact BPE token count for s.
func (c *BPECounter) CountString(s string) int {
	if s == "" {
		return 0
	}
	n, err := c.codec.Count(s)
	if err != nil {
		return c.fallback.CountString(s)
	}
	return n
}

// CountTokens returns the token count for a full set of chat messages,
// including per-message and conversation framing overhead.
func (c *BPECounter) CountTokens(messages []Message) int {
	total := 0
	for i := range messages {
		total += perMessageOverhead
		total += c.CountString(messages[i].Content)
		if messages[i].Name != "" {
			total += c.CountString(messages[i].Name)
		}
		for _, tc := range messages[i].ToolCalls {
			total += c.CountString(tc.Name)
			total += c.CountString(tc.Arguments)
		}
	}
	total += conversationOverhead
	return total
}

// EstimatingCounter uses a character-ratio heuristic (1 token ~ 4 chars)
// to estimate token counts without external dependencies.
type EstimatingCounter struct {
	CharsPerToken float64
}

// NewEstimatingCounter returns a counter using the default 4-chars-per-token ratio.
func NewEstimatingCounter() *EstimatingCounter {
	return &EstimatingCounter{CharsPerToken: 4.0}
}

func (c *EstimatingCounter) CountTokens(messages []Message) int {
	total := 0
	for i := range messages {
		// Per-message overhead (role, separators) ~4 tokens
		total += 4
		total += c.CountString(messages[i].Content)
		if messages[i].Name != "" {
			total += c.CountString(messages[i].Name)
		}
		for _, tc := range messages[i].ToolCalls {
			total += c.CountString(tc.Name)
			total += c.CountString(tc.Arguments)
		}
	}
	// Conversation framing overhead
	total += 3
	return total
}

func (c *EstimatingCounter) CountString(s string) int {
	if s == "" {
		return 0
	}
	cpt := c.CharsPerToken
	if cpt <= 0 {
		cpt = 4.0
	}
	return int(float64(len(s))/cpt) + 1
}

// ContextLimit returns the maximum context window (in tokens) for a model.
// If the model is unknown, it returns the provided fallback value.
func ContextLimit(modelName string, fallback int) int {
	if limit, ok := modelContextLimits[modelName]; ok {
		return limit
	}
	if fallback > 0 {
		return fallback
	}
	return defaultContextLimit
}

const defaultContextLimit = 8192

// modelContextLimits maps well-known model identifiers to their context window sizes.
var modelContextLimits = map[string]int{
	// OpenAI
	"gpt-5.5":       1000000,
	"gpt-5.5-pro":   1000000,
	"gpt-5":         400000,
	"gpt-4o":        128000,
	"gpt-4o-mini":   128000,
	"gpt-4-turbo":   128000,
	"gpt-4":         8192,
	"gpt-4-32k":     32768,
	"gpt-3.5-turbo": 16385,
	"o1":            200000,
	"o1-mini":       128000,
	"o1-preview":    128000,
	"o3":            200000,
	"o3-mini":       200000,
	"o4-mini":       200000,

	// Anthropic
	"claude-fable-5":    1000000,
	"claude-opus-4-8":   1000000,
	"claude-opus-4-7":   1000000,
	"claude-sonnet-5":   1000000,
	"claude-haiku-4-5":  200000,
	"claude-sonnet-4-6": 200000,
	"claude-sonnet-4-5": 200000,
	"claude-3-5-sonnet": 200000,
	"claude-3-opus":     200000,
	"claude-3-haiku":    200000,
	"claude-3-5-haiku":  200000,

	// Google Gemini
	"gemini-3.5-flash":      1048576,
	"gemini-3.1-pro":        1048576,
	"gemini-3-flash":        1048576,
	"gemini-3.1-flash-lite": 1048576,
	"gemini-2.0-flash":      1048576,
	"gemini-2.0-pro":        1048576,
	"gemini-1.5-flash":      1048576,
	"gemini-1.5-pro":        2097152,

	// Mistral
	"mistral-large-latest":  128000,
	"mistral-medium-latest": 32768,
	"mistral-small-latest":  32768,
	"codestral-latest":      32768,

	// Meta (via Ollama or hosted)
	"llama3.3": 131072,
	"llama3.2": 131072,
	"llama3.1": 131072,
	"llama3":   8192,

	// DeepSeek
	"deepseek-chat":     64000,
	"deepseek-coder":    64000,
	"deepseek-reasoner": 64000,
}
