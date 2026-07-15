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
	pending map[string][]time.Time // key -> FIFO stack of start times
}

// NewMetricsHook creates a new metrics hook.
func NewMetricsHook() *MetricsHook {
	return &MetricsHook{
		pending: make(map[string][]time.Time),
	}
}

func (h *MetricsHook) Before(_ context.Context, evt *Event) error {
	switch evt.Type {
	case EventModelCallBefore, EventToolCallBefore:
		h.mu.Lock()
		pushPending(h.pending, metricsKey(evt))
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

	started, ok := popPending(h.pending, metricsKey(evt))
	if !ok {
		started = time.Now()
	}

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
	h.pending = make(map[string][]time.Time)
}

func metricsKey(evt *Event) string {
	return string(evt.Type) + ":" + evt.Name
}

// pushPending records a start time for key. Callers must hold the hook's mutex.
func pushPending(m map[string][]time.Time, key string) {
	m[key] = append(m[key], time.Now())
}

// popPending removes and returns the oldest start time for key (FIFO), so
// concurrent same-named calls each pair with a real start time instead of
// colliding on a single map slot. Callers must hold the hook's mutex.
func popPending(m map[string][]time.Time, key string) (time.Time, bool) {
	s := m[key]
	if len(s) == 0 {
		return time.Time{}, false
	}
	t := s[0]
	if len(s) == 1 {
		delete(m, key)
	} else {
		m[key] = s[1:]
	}
	return t, true
}

// MetricsSink is the minimal metrics interface that PrometheusHook feeds during
// execution. It is defined here (rather than importing os/metrics) so the
// engine layer stays free of a dependency on the control plane; the integrator
// wires a concrete sink such as *metrics.Registry, which satisfies this
// interface. Tenant is plumbed from context so cost/latency/tokens can be
// attributed per tenant.
type MetricsSink interface {
	RecordToolCall(tenant, tool string, d time.Duration, isErr bool)
	RecordModelCall(tenant, provider string, d time.Duration, promptTokens, completionTokens int64, isErr bool)
}

// tenantContextKey carries a tenant identifier through context for metric
// attribution. Kept unexported to avoid collisions.
type tenantContextKey struct{}

// WithTenant returns a context carrying the tenant id used for metric labels.
func WithTenant(ctx context.Context, tenant string) context.Context {
	return context.WithValue(ctx, tenantContextKey{}, tenant)
}

// TenantFromContext returns the tenant id from context, or "" if unset.
func TenantFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return v
	}
	return ""
}

// PrometheusHook bridges execution events to a MetricsSink (e.g. the ChronosOS
// Prometheus registry) so tool calls, model calls, tokens, errors, and latency
// are recorded as execution happens. It tracks per-call start times to compute
// durations across the Before/After boundary.
type PrometheusHook struct {
	sink    MetricsSink
	mu      sync.Mutex
	pending map[string][]time.Time
}

// NewPrometheusHook creates a hook that feeds sink. A nil sink makes the hook a
// no-op (safe to install unconditionally).
func NewPrometheusHook(sink MetricsSink) *PrometheusHook {
	return &PrometheusHook{
		sink:    sink,
		pending: make(map[string][]time.Time),
	}
}

func (h *PrometheusHook) Before(_ context.Context, evt *Event) error {
	switch evt.Type {
	case EventModelCallBefore, EventToolCallBefore:
		h.mu.Lock()
		pushPending(h.pending, metricsKey(evt))
		h.mu.Unlock()
	}
	return nil
}

func (h *PrometheusHook) After(ctx context.Context, evt *Event) error {
	if h.sink == nil {
		return nil
	}
	switch evt.Type {
	case EventModelCallAfter, EventToolCallAfter:
	default:
		return nil
	}

	h.mu.Lock()
	started, ok := popPending(h.pending, metricsKey(evt))
	h.mu.Unlock()
	if !ok {
		started = time.Now()
	}

	dur := time.Since(started)
	tenant := TenantFromContext(ctx)
	isErr := evt.Error != nil

	switch evt.Type {
	case EventToolCallAfter:
		h.sink.RecordToolCall(tenant, evt.Name, dur, isErr)
	case EventModelCallAfter:
		provider := providerFromEvent(evt)
		prompt, completion := tokensFromEvent(evt)
		h.sink.RecordModelCall(tenant, provider, dur, prompt, completion, isErr)
	}
	return nil
}

// providerFromEvent extracts the model provider name from event metadata,
// falling back to the event name (the model id) when absent.
func providerFromEvent(evt *Event) string {
	if evt.Metadata != nil {
		if p, ok := evt.Metadata["provider"].(string); ok && p != "" {
			return p
		}
	}
	return evt.Name
}

// tokensFromEvent reads prompt/completion token counts from event metadata.
// It tolerates both int and int64 values.
func tokensFromEvent(evt *Event) (prompt, completion int64) {
	if evt.Metadata == nil {
		return 0, 0
	}
	prompt = metaInt64(evt.Metadata["prompt_tokens"])
	completion = metaInt64(evt.Metadata["completion_tokens"])
	return prompt, completion
}

func metaInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	default:
		return 0
	}
}
