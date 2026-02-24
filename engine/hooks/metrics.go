package hooks

import (
	"context"
	"sync"
	"time"
)

// CallMetric records timing and usage data for a single model or tool call.
type CallMetric struct {
	Type             EventType     `json:"type"`
	Name             string        `json:"name"`
	StartedAt        time.Time     `json:"started_at"`
	Duration         time.Duration `json:"duration"`
	PromptTokens     int           `json:"prompt_tokens,omitempty"`
	CompletionTokens int           `json:"completion_tokens,omitempty"`
	Error            bool          `json:"error,omitempty"`
}

// MetricsSummary aggregates metrics across all calls.
type MetricsSummary struct {
	TotalModelCalls   int           `json:"total_model_calls"`
	TotalToolCalls    int           `json:"total_tool_calls"`
	TotalErrors       int           `json:"total_errors"`
	TotalPromptTokens int           `json:"total_prompt_tokens"`
	TotalCompTokens   int           `json:"total_completion_tokens"`
	AvgModelLatency   time.Duration `json:"avg_model_latency"`
	AvgToolLatency    time.Duration `json:"avg_tool_latency"`
	MaxModelLatency   time.Duration `json:"max_model_latency"`
	MaxToolLatency    time.Duration `json:"max_tool_latency"`
}

// MetricsHook provides structured observability for model and tool calls.
// It tracks latency, token usage, and error rates with thread-safe counters.
type MetricsHook struct {
	mu      sync.Mutex
	calls   []CallMetric
	pending map[string]time.Time // event name -> start time
}

// NewMetricsHook creates a new metrics hook.
func NewMetricsHook() *MetricsHook {
	return &MetricsHook{
		pending: make(map[string]time.Time),
	}
}

func (h *MetricsHook) Before(_ context.Context, evt *Event) error {
	switch evt.Type {
	case EventModelCallBefore, EventToolCallBefore:
		h.mu.Lock()
		h.pending[metricsKey(evt)] = time.Now()
		h.mu.Unlock()
	}
	return nil
}

func (h *MetricsHook) After(_ context.Context, evt *Event) error {
	switch evt.Type {
	case EventModelCallAfter, EventToolCallAfter:
	default:
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	key := metricsKey(evt)
	started, ok := h.pending[key]
	if !ok {
		started = time.Now()
	}
	delete(h.pending, key)

	metric := CallMetric{
		Type:      evt.Type,
		Name:      evt.Name,
		StartedAt: started,
		Duration:  time.Since(started),
		Error:     evt.Error != nil,
	}

	if evt.Metadata != nil {
		if p, ok := evt.Metadata["prompt_tokens"].(int); ok {
			metric.PromptTokens = p
		}
		if c, ok := evt.Metadata["completion_tokens"].(int); ok {
			metric.CompletionTokens = c
		}
	}

	h.calls = append(h.calls, metric)
	return nil
}

// GetMetrics returns all recorded call metrics.
func (h *MetricsHook) GetMetrics() []CallMetric {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]CallMetric, len(h.calls))
	copy(result, h.calls)
	return result
}

// GetSummary computes an aggregated summary of all recorded metrics.
func (h *MetricsHook) GetSummary() MetricsSummary {
	h.mu.Lock()
	defer h.mu.Unlock()

	var s MetricsSummary
	var totalModelDur, totalToolDur time.Duration

	for _, c := range h.calls {
		switch c.Type {
		case EventModelCallAfter:
			s.TotalModelCalls++
			totalModelDur += c.Duration
			if c.Duration > s.MaxModelLatency {
				s.MaxModelLatency = c.Duration
			}
			s.TotalPromptTokens += c.PromptTokens
			s.TotalCompTokens += c.CompletionTokens
		case EventToolCallAfter:
			s.TotalToolCalls++
			totalToolDur += c.Duration
			if c.Duration > s.MaxToolLatency {
				s.MaxToolLatency = c.Duration
			}
		}
		if c.Error {
			s.TotalErrors++
		}
	}

	if s.TotalModelCalls > 0 {
		s.AvgModelLatency = totalModelDur / time.Duration(s.TotalModelCalls)
	}
	if s.TotalToolCalls > 0 {
		s.AvgToolLatency = totalToolDur / time.Duration(s.TotalToolCalls)
	}
	return s
}

// Reset clears all recorded metrics.
func (h *MetricsHook) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = nil
	h.pending = make(map[string]time.Time)
}

func metricsKey(evt *Event) string {
	return string(evt.Type) + ":" + evt.Name
}
