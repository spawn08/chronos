package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistry_ReusesExistingMetrics(t *testing.T) {
	r := NewRegistry()
	c1 := r.Counter("same_counter", "help1")
	c2 := r.Counter("same_counter", "ignored help")
	if c1 != c2 {
		t.Fatal("Counter should return same instance for same name")
	}
	g1 := r.Gauge("same_gauge", "g1")
	g2 := r.Gauge("same_gauge", "g2")
	if g1 != g2 {
		t.Fatal("Gauge should return same instance for same name")
	}
	_ = g2
	h1 := r.Histogram("same_hist", "h1", []float64{1, 2})
	h2 := r.Histogram("same_hist", "h2", []float64{9, 8})
	if h1 != h2 {
		t.Fatal("Histogram should return same instance for same name")
	}
}

func TestGauge_writeTo_WithoutSet(t *testing.T) {
	r := NewRegistry()
	_ = r.Gauge("fresh_gauge", "never set")
	req := httptest.NewRequest(http.MethodGet, "/m", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "fresh_gauge") {
		t.Fatalf("expected fresh_gauge in output:\n%s", body)
	}
}

func TestHistogram_writeTo_WithoutObserve(t *testing.T) {
	r := NewRegistry()
	_ = r.Histogram("empty_histo", "no observations", []float64{0.5, 1})
	req := httptest.NewRequest(http.MethodGet, "/m", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	if !strings.Contains(w.Body.String(), "empty_histo_count 0") {
		t.Errorf("expected zero count histogram line in:\n%s", w.Body.String())
	}
}

func TestCounter_writeTo_LabeledAndUnlabeledMix(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("mix_counter", "mix")
	c.Inc(nil)
	c.Inc(map[string]string{"k": "v"})
	req := httptest.NewRequest(http.MethodGet, "/m", nil)
	w := httptest.NewRecorder()
	r.Handler().ServeHTTP(w, req)
	out := w.Body.String()
	if !strings.Contains(out, `mix_counter{k="v"} 1`) {
		t.Errorf("expected labeled line: %s", out)
	}
}
