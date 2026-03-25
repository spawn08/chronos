// Package metrics provides Prometheus-format metrics collection and export.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
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

// Histogram tracks value distributions in configurable buckets.
type Histogram struct {
	name    string
	help    string
	buckets []float64
	mu      sync.Mutex
	counts  []int64 // per-bucket counts
	sum     float64
	count   int64
}

func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.counts == nil {
		h.counts = make([]int64, len(h.buckets))
	}
	h.sum += v
	h.count++
	for i, b := range h.buckets {
		if v <= b {
			h.counts[i]++
		}
	}
}

func (h *Histogram) writeTo(b *strings.Builder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(b, "# HELP %s %s\n", h.name, h.help)
	fmt.Fprintf(b, "# TYPE %s histogram\n", h.name)
	if h.counts == nil {
		h.counts = make([]int64, len(h.buckets))
	}
	var cumulative int64
	for i, bucket := range h.buckets {
		cumulative += h.counts[i]
		fmt.Fprintf(b, "%s_bucket{le=\"%g\"} %d\n", h.name, bucket, cumulative)
	}
	fmt.Fprintf(b, "%s_bucket{le=\"+Inf\"} %d\n", h.name, h.count)
	fmt.Fprintf(b, "%s_sum %g\n", h.name, h.sum)
	fmt.Fprintf(b, "%s_count %d\n", h.name, h.count)
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
