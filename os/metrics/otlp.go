package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OTLPExporter exports the registry's metrics to an OTLP/HTTP endpoint using
// the JSON encoding of the OpenTelemetry metrics protocol
// (ExportMetricsServiceRequest). When the endpoint is empty it is a no-op, so
// offline runs and tests never require a live collector.
type OTLPExporter struct {
	endpoint string
	client   *http.Client
	headers  map[string]string
}

// NewOTLPExporter creates a metrics exporter. endpoint should be the collector
// base URL (e.g. "http://localhost:4318"); the exporter POSTs to
// "<endpoint>/v1/metrics". An empty endpoint yields a no-op exporter.
func NewOTLPExporter(endpoint string) *OTLPExporter {
	return &OTLPExporter{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 10 * time.Second},
		headers:  map[string]string{},
	}
}

// WithHTTPClient overrides the HTTP client (useful for tests).
func (e *OTLPExporter) WithHTTPClient(c *http.Client) *OTLPExporter {
	e.client = c
	return e
}

// WithHeader adds a header sent with each export request (e.g. auth).
func (e *OTLPExporter) WithHeader(k, v string) *OTLPExporter {
	e.headers[k] = v
	return e
}

// Enabled reports whether the exporter will actually export.
func (e *OTLPExporter) Enabled() bool { return e.endpoint != "" }

// Export snapshots the registry and sends it to the configured endpoint. When
// disabled (empty endpoint) it returns nil without doing any I/O.
func (e *OTLPExporter) Export(ctx context.Context, r *Registry) error {
	if !e.Enabled() {
		return nil
	}
	payload := buildOTLPMetrics(r)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal otlp metrics: %w", err)
	}

	url := e.endpoint + "/v1/metrics"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build otlp metrics request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range e.headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("export otlp metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("export otlp metrics: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// --- Minimal OTLP/JSON metrics structures ---

type otlpMetricsRequest struct {
	ResourceMetrics []otlpResourceMetrics `json:"resourceMetrics"`
}

type otlpResourceMetrics struct {
	ScopeMetrics []otlpScopeMetrics `json:"scopeMetrics"`
}

type otlpScopeMetrics struct {
	Metrics []otlpMetric `json:"metrics"`
}

type otlpMetric struct {
	Name      string        `json:"name"`
	Sum       *otlpSum      `json:"sum,omitempty"`
	Gauge     *otlpGauge    `json:"gauge,omitempty"`
	Histogram *otlpHistoAgg `json:"histogram,omitempty"`
}

type otlpSum struct {
	AggregationTemporality int            `json:"aggregationTemporality"`
	IsMonotonic            bool           `json:"isMonotonic"`
	DataPoints             []otlpNumberDP `json:"dataPoints"`
}

type otlpGauge struct {
	DataPoints []otlpNumberDP `json:"dataPoints"`
}

type otlpHistoAgg struct {
	AggregationTemporality int           `json:"aggregationTemporality"`
	DataPoints             []otlpHistoDP `json:"dataPoints"`
}

type otlpNumberDP struct {
	Attributes []otlpKV `json:"attributes,omitempty"`
	AsDouble   float64  `json:"asDouble"`
}

type otlpHistoDP struct {
	Attributes     []otlpKV  `json:"attributes,omitempty"`
	Count          uint64    `json:"count"`
	Sum            float64   `json:"sum"`
	BucketCounts   []uint64  `json:"bucketCounts"`
	ExplicitBounds []float64 `json:"explicitBounds"`
}

type otlpKV struct {
	Key   string      `json:"key"`
	Value otlpKVValue `json:"value"`
}

type otlpKVValue struct {
	StringValue string `json:"stringValue"`
}

func attrsFromLabels(labels map[string]string) []otlpKV {
	if len(labels) == 0 {
		return nil
	}
	out := make([]otlpKV, 0, len(labels))
	for k, v := range labels {
		out = append(out, otlpKV{Key: k, Value: otlpKVValue{StringValue: v}})
	}
	return out
}

// buildOTLPMetrics converts the registry into an OTLP metrics request. The
// aggregation temporality is cumulative (2).
func buildOTLPMetrics(r *Registry) otlpMetricsRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make([]otlpMetric, 0, len(r.counters)+len(r.gauges)+len(r.histos))

	for _, c := range r.counters {
		c.mu.Lock()
		dps := make([]otlpNumberDP, 0, len(c.labels))
		for key, v := range c.labels {
			dps = append(dps, otlpNumberDP{
				Attributes: attrsFromLabels(deserializeLabels(key)),
				AsDouble:   float64(v),
			})
		}
		if len(dps) == 0 {
			dps = append(dps, otlpNumberDP{AsDouble: float64(c.value)})
		}
		c.mu.Unlock()
		metrics = append(metrics, otlpMetric{
			Name: c.name,
			Sum:  &otlpSum{AggregationTemporality: 2, IsMonotonic: true, DataPoints: dps},
		})
	}

	for _, g := range r.gauges {
		g.mu.Lock()
		dps := make([]otlpNumberDP, 0, len(g.labels))
		for key, v := range g.labels {
			dps = append(dps, otlpNumberDP{
				Attributes: attrsFromLabels(deserializeLabels(key)),
				AsDouble:   v,
			})
		}
		if len(dps) == 0 {
			dps = append(dps, otlpNumberDP{AsDouble: g.value})
		}
		g.mu.Unlock()
		metrics = append(metrics, otlpMetric{
			Name:  g.name,
			Gauge: &otlpGauge{DataPoints: dps},
		})
	}

	for _, h := range r.histos {
		h.mu.Lock()
		dps := make([]otlpHistoDP, 0, len(h.series)+1)
		if h.def.count > 0 {
			dps = append(dps, histoDataPoint(h, nil, h.def))
		}
		for key, s := range h.series {
			dps = append(dps, histoDataPoint(h, deserializeLabels(key), s))
		}
		h.mu.Unlock()
		metrics = append(metrics, otlpMetric{
			Name:      h.name,
			Histogram: &otlpHistoAgg{AggregationTemporality: 2, DataPoints: dps},
		})
	}

	return otlpMetricsRequest{
		ResourceMetrics: []otlpResourceMetrics{{
			ScopeMetrics: []otlpScopeMetrics{{Metrics: metrics}},
		}},
	}
}

// histoDataPoint converts a cumulative-per-le series into an OTLP histogram
// data point with per-bucket (non-cumulative) counts.
func histoDataPoint(h *Histogram, labels map[string]string, s histoSeries) otlpHistoDP {
	bucketCounts := make([]uint64, len(h.buckets)+1)
	var prev int64
	for i := range h.buckets {
		var c int64
		if s.counts != nil {
			c = s.counts[i]
		}
		bucketCounts[i] = uint64(c - prev)
		prev = c
	}
	bucketCounts[len(h.buckets)] = uint64(s.count - prev)
	return otlpHistoDP{
		Attributes:     attrsFromLabels(labels),
		Count:          uint64(s.count),
		Sum:            s.sum,
		BucketCounts:   bucketCounts,
		ExplicitBounds: h.buckets,
	}
}
