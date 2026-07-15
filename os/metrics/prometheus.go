// Package metrics provides Prometheus-format metrics collection and export.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry holds all Chronos metrics and serves them in Prometheus format.
type Registry struct {
	mu       sync.RWMutex
	counters map[string]*Counter
	gauges   map[string]*Gauge
	histos   map[string]*Histogram
}

// NewRegistry creates a new metrics registry with pre-defined Chronos metrics.
func NewRegistry() *Registry {
	r := &Registry{
		counters: make(map[string]*Counter),
		gauges:   make(map[string]*Gauge),
		histos:   make(map[string]*Histogram),
	}

	// Pre-register Chronos metrics
	r.Counter("chronos_agent_runs_total", "Total number of agent runs")
	r.Counter("chronos_tool_calls_total", "Total number of tool calls")
	r.Counter("chronos_tokens_used_total", "Total tokens used across all providers")
	r.Counter("chronos_model_calls_total", "Total model API calls")
	r.Counter("chronos_errors_total", "Total error count")
	r.Gauge("chronos_active_sessions", "Number of currently active sessions")
	r.Histogram("chronos_model_latency_seconds", "Model call latency in seconds",
		[]float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10})
	r.Histogram("chronos_tool_latency_seconds", "Tool call latency in seconds",
		[]float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10})

	return r
}

// Counter returns or creates a counter metric.
func (r *Registry) Counter(name, help string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{name: name, help: help, labels: make(map[string]int64)}
	r.counters[name] = c
	return c
}

// Gauge returns or creates a gauge metric.
func (r *Registry) Gauge(name, help string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{name: name, help: help, labels: make(map[string]float64)}
	r.gauges[name] = g
	return g
}

// Histogram returns or creates a histogram metric.
func (r *Registry) Histogram(name, help string, buckets []float64) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histos[name]; ok {
		return h
	}
	sort.Float64s(buckets)
	h := &Histogram{name: name, help: help, buckets: buckets}
	r.histos[name] = h
	return h
}

// Handler returns an http.Handler that serves metrics in Prometheus format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		r.mu.RLock()
		defer r.mu.RUnlock()

		var b strings.Builder

		// Counters
		for _, c := range r.counters {
			c.writeTo(&b)
		}
		// Gauges
		for _, g := range r.gauges {
			g.writeTo(&b)
		}
		// Histograms
		for _, h := range r.histos {
			h.writeTo(&b)
		}

		_, _ = w.Write([]byte(b.String()))
	})
}

// IncAgentRuns increments the agent runs counter.
func (r *Registry) IncAgentRuns(agentID string) {
	r.counters["chronos_agent_runs_total"].Inc(map[string]string{"agent_id": agentID})
}

// IncToolCalls increments the tool calls counter.
func (r *Registry) IncToolCalls(toolName string) {
	r.counters["chronos_tool_calls_total"].Inc(map[string]string{"tool": toolName})
}

// AddTokens adds to the token usage counter.
func (r *Registry) AddTokens(provider string, count int64) {
	r.counters["chronos_tokens_used_total"].Add(count, map[string]string{"provider": provider})
}

// ObserveModelLatency records a model call latency.
func (r *Registry) ObserveModelLatency(provider string, d time.Duration) {
	r.histos["chronos_model_latency_seconds"].Observe(d.Seconds())
	r.counters["chronos_model_calls_total"].Inc(map[string]string{"provider": provider})
}

// SetActiveSessions sets the active session count.
func (r *Registry) SetActiveSessions(n float64) {
	r.gauges["chronos_active_sessions"].Set(n)
}

// normalizeTenant maps an empty tenant to a stable label value so that
// per-tenant series always carry the dimension (avoids ""-keyed drift).
func normalizeTenant(tenant string) string {
	if tenant == "" {
		return "default"
	}
	return tenant
}

// RecordToolCall records a single tool invocation with per-tenant attribution:
// it increments the tool-call counter, observes latency, and increments the
// error counter when the call failed.
func (r *Registry) RecordToolCall(tenant, tool string, d time.Duration, isErr bool) {
	tenant = normalizeTenant(tenant)
	labels := map[string]string{"tenant": tenant, "tool": tool}
	r.counters["chronos_tool_calls_total"].Inc(labels)
	r.histos["chronos_tool_latency_seconds"].ObserveWithLabels(d.Seconds(), labels)
	if isErr {
		r.counters["chronos_errors_total"].Inc(map[string]string{"tenant": tenant, "kind": "tool"})
	}
}

// RecordModelCall records a single model invocation with per-tenant attribution:
// model-call counter, token usage (prompt + completion), latency, and errors.
func (r *Registry) RecordModelCall(tenant, provider string, d time.Duration, promptTokens, completionTokens int64, isErr bool) {
	tenant = normalizeTenant(tenant)
	base := map[string]string{"tenant": tenant, "provider": provider}
	r.counters["chronos_model_calls_total"].Inc(base)
	r.histos["chronos_model_latency_seconds"].ObserveWithLabels(d.Seconds(), base)
	if promptTokens > 0 {
		r.counters["chronos_tokens_used_total"].Add(promptTokens,
			map[string]string{"tenant": tenant, "provider": provider, "type": "prompt"})
	}
	if completionTokens > 0 {
		r.counters["chronos_tokens_used_total"].Add(completionTokens,
			map[string]string{"tenant": tenant, "provider": provider, "type": "completion"})
	}
	if isErr {
		r.counters["chronos_errors_total"].Inc(map[string]string{"tenant": tenant, "kind": "model"})
	}
}

// IncErrors increments the error counter for a tenant and error kind.
func (r *Registry) IncErrors(tenant, kind string) {
	r.counters["chronos_errors_total"].Inc(map[string]string{"tenant": normalizeTenant(tenant), "kind": kind})
}

// Counter is a monotonically increasing metric.
type Counter struct {
	name   string
	help   string
	mu     sync.Mutex
	value  int64
	labels map[string]int64 // serialized labels -> value
}

func (c *Counter) Inc(labels map[string]string) {
	c.Add(1, labels)
}

func (c *Counter) Add(n int64, labels map[string]string) {
	key := serializeLabels(labels)
	c.mu.Lock()
	c.labels[key] += n
	atomic.AddInt64(&c.value, n)
	c.mu.Unlock()
}

func (c *Counter) writeTo(b *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n", c.name, c.help)
	fmt.Fprintf(b, "# TYPE %s counter\n", c.name)
	if len(c.labels) == 0 {
		fmt.Fprintf(b, "%s %d\n", c.name, c.value)
	} else {
		for k, v := range c.labels {
			if k == "" {
				fmt.Fprintf(b, "%s %d\n", c.name, v)
			} else {
				fmt.Fprintf(b, "%s{%s} %d\n", c.name, k, v)
			}
		}
	}
}

// Gauge is a metric that can go up and down.
type Gauge struct {
	name   string
	help   string
	mu     sync.Mutex
	value  float64
	labels map[string]float64
}

func (g *Gauge) Set(v float64) {
	g.mu.Lock()
	g.value = v
	g.labels[""] = v
	g.mu.Unlock()
}

func (g *Gauge) writeTo(b *strings.Builder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n", g.name, g.help)
	fmt.Fprintf(b, "# TYPE %s gauge\n", g.name)
	if len(g.labels) == 0 {
		fmt.Fprintf(b, "%s %g\n", g.name, g.value)
	} else {
		for k, v := range g.labels {
			if k == "" {
				fmt.Fprintf(b, "%s %g\n", g.name, v)
			} else {
				fmt.Fprintf(b, "%s{%s} %g\n", g.name, k, v)
			}
		}
	}
}

// Histogram tracks value distributions in configurable buckets. It keeps an
// unlabeled default series plus any number of labeled series (used for
// per-tenant / per-provider attribution).
type Histogram struct {
	name    string
	help    string
	buckets []float64
	mu      sync.Mutex
	def     histoSeries            // unlabeled series
	series  map[string]histoSeries // serialized labels -> series
}

// histoSeries holds the per-bucket counts, sum, and total count for one
// label set. counts[i] is the cumulative number of observations <= buckets[i]
// (Prometheus "le" semantics), so it can be written out directly.
type histoSeries struct {
	counts []int64
	sum    float64
	count  int64
}

func (s *histoSeries) observe(v float64, buckets []float64) {
	if s.counts == nil {
		s.counts = make([]int64, len(buckets))
	}
	s.sum += v
	s.count++
	for i, b := range buckets {
		if v <= b {
			s.counts[i]++
		}
	}
}

// Observe records a value in the unlabeled series.
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.def.observe(v, h.buckets)
}

// ObserveWithLabels records a value in the series identified by labels. An
// empty label set falls back to the unlabeled series.
func (h *Histogram) ObserveWithLabels(v float64, labels map[string]string) {
	key := serializeLabels(labels)
	h.mu.Lock()
	defer h.mu.Unlock()
	if key == "" {
		h.def.observe(v, h.buckets)
		return
	}
	if h.series == nil {
		h.series = make(map[string]histoSeries)
	}
	s := h.series[key]
	s.observe(v, h.buckets)
	h.series[key] = s
}

func (h *Histogram) writeTo(b *strings.Builder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n", h.name, h.help)
	fmt.Fprintf(b, "# TYPE %s histogram\n", h.name)
	// Unlabeled series is always emitted (count may be 0).
	h.writeSeries(b, "", h.def)
	// Labeled series, sorted for deterministic output.
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.writeSeries(b, k, h.series[k])
	}
}

// writeSeries emits the bucket, _sum, and _count lines for a single series.
// baseLabels is the pre-serialized label set ("" for the unlabeled series).
func (h *Histogram) writeSeries(b *strings.Builder, baseLabels string, s histoSeries) {
	counts := s.counts
	if counts == nil {
		counts = make([]int64, len(h.buckets))
	}
	for i, bucket := range h.buckets {
		fmt.Fprintf(b, "%s_bucket{%s} %d\n", h.name, mergeLabels(baseLabels, fmt.Sprintf("le=%q", formatFloat(bucket))), counts[i])
	}
	fmt.Fprintf(b, "%s_bucket{%s} %d\n", h.name, mergeLabels(baseLabels, `le="+Inf"`), s.count)
	if baseLabels == "" {
		fmt.Fprintf(b, "%s_sum %g\n", h.name, s.sum)
		fmt.Fprintf(b, "%s_count %d\n", h.name, s.count)
	} else {
		fmt.Fprintf(b, "%s_sum{%s} %g\n", h.name, baseLabels, s.sum)
		fmt.Fprintf(b, "%s_count{%s} %d\n", h.name, baseLabels, s.count)
	}
}

// mergeLabels joins a base label string with an extra label, handling the
// empty-base case.
func mergeLabels(base, extra string) string {
	if base == "" {
		return extra
	}
	return base + "," + extra
}

// formatFloat renders a bucket boundary the same way %g does, used inside the
// le="..." label.
func formatFloat(v float64) string {
	return fmt.Sprintf("%g", v)
}

func serializeLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%q", k, labels[k])
	}
	return strings.Join(parts, ",")
}

// deserializeLabels parses a serialized label key (as produced by
// serializeLabels) back into a map. It tolerates values containing commas
// because it splits on the delimiter between quoted values.
func deserializeLabels(key string) map[string]string {
	if key == "" {
		return nil
	}
	out := make(map[string]string)
	rest := key
	for rest != "" {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		name := rest[:eq]
		rest = rest[eq+1:]
		if rest == "" || rest[0] != '"' {
			break
		}
		// Find the closing quote, skipping escaped quotes.
		i := 1
		for i < len(rest) {
			if rest[i] == '\\' {
				i += 2
				continue
			}
			if rest[i] == '"' {
				break
			}
			i++
		}
		if i >= len(rest) {
			break
		}
		val, err := strconv.Unquote(rest[:i+1])
		if err != nil {
			val = rest[1:i]
		}
		out[name] = val
		rest = strings.TrimPrefix(rest[i+1:], ",")
	}
	return out
}
