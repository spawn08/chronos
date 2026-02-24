package model

// TokenCounter estimates the token count for a set of messages.
type TokenCounter interface {
	CountTokens(messages []Message) int
	CountString(s string) int
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
	for _, m := range messages {
		// Per-message overhead (role, separators) ~4 tokens
		total += 4
		total += c.CountString(m.Content)
		if m.Name != "" {
			total += c.CountString(m.Name)
		}
		for _, tc := range m.ToolCalls {
			total += c.CountString(tc.Name)
			total += c.CountString(tc.Arguments)
		}
	}
	// Conversation framing overhead
	total += 3
	return total
}

func (c *EstimatingCounter) CountString(s string) int {
	if len(s) == 0 {
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
	"claude-sonnet-4-6": 200000,
	"claude-sonnet-4-5": 200000,
	"claude-3-5-sonnet": 200000,
	"claude-3-opus":     200000,
	"claude-3-haiku":    200000,
	"claude-3-5-haiku":  200000,

	// Google Gemini
	"gemini-2.0-flash": 1048576,
	"gemini-2.0-pro":   1048576,
	"gemini-1.5-flash": 1048576,
	"gemini-1.5-pro":   2097152,

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
