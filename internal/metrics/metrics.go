// Package metrics provides a tiny dependency-free metrics registry that
// exposes counters, gauges, and latency quantiles in Prometheus text format.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing value.
type Counter struct{ v atomic.Int64 }

// Inc adds one to the counter.
func (c *Counter) Inc() { c.v.Add(1) }

// Add adds n to the counter.
func (c *Counter) Add(n int64) { c.v.Add(n) }

// Value returns the current count.
func (c *Counter) Value() int64 { return c.v.Load() }

// Gauge is a value that can go up or down.
type Gauge struct{ v atomic.Int64 }

// Set replaces the gauge value.
func (g *Gauge) Set(n int64) { g.v.Store(n) }

// Add adds n (may be negative) to the gauge.
func (g *Gauge) Add(n int64) { g.v.Add(n) }

// Value returns the current gauge value.
func (g *Gauge) Value() int64 { return g.v.Load() }

// Histogram tracks observed latencies in a bounded ring buffer and reports
// quantiles. It is intentionally simple: good enough for p50/p95/p99 on the
// scale this platform benchmarks at.
type Histogram struct {
	mu      sync.Mutex
	samples []float64
	cap     int
	idx     int
	count   int64
	sum     float64
}

// NewHistogram creates a histogram retaining the last capacity samples.
func NewHistogram(capacity int) *Histogram {
	if capacity <= 0 {
		capacity = 1024
	}
	return &Histogram{samples: make([]float64, 0, capacity), cap: capacity}
}

// ObserveDuration records a duration in milliseconds.
func (h *Histogram) ObserveDuration(d time.Duration) { h.Observe(float64(d.Microseconds()) / 1000.0) }

// Observe records a value (milliseconds by convention).
func (h *Histogram) Observe(v float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += v
	if len(h.samples) < h.cap {
		h.samples = append(h.samples, v)
		return
	}
	h.samples[h.idx] = v
	h.idx = (h.idx + 1) % h.cap
}

// Quantile returns the requested quantile (0..1) over retained samples.
func (h *Histogram) Quantile(q float64) float64 {
	h.mu.Lock()
	cp := make([]float64, len(h.samples))
	copy(cp, h.samples)
	h.mu.Unlock()
	if len(cp) == 0 {
		return 0
	}
	sort.Float64s(cp)
	idx := int(q * float64(len(cp)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}

// Registry holds named metrics and renders them in Prometheus text format.
type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	histograms map[string]*Histogram
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*Counter{},
		gauges:     map[string]*Gauge{},
		histograms: map[string]*Histogram{},
	}
}

// Counter returns (creating if needed) a named counter.
func (r *Registry) Counter(name string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{}
	r.counters[name] = c
	return c
}

// Gauge returns (creating if needed) a named gauge.
func (r *Registry) Gauge(name string) *Gauge {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g
	}
	g := &Gauge{}
	r.gauges[name] = g
	return g
}

// Histogram returns (creating if needed) a named histogram.
func (r *Registry) Histogram(name string, capacity int) *Histogram {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h
	}
	h := NewHistogram(capacity)
	r.histograms[name] = h
	return h
}

// Handler serves the registry in Prometheus exposition format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for name, c := range r.counters {
			fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, c.Value())
		}
		for name, g := range r.gauges {
			fmt.Fprintf(w, "# TYPE %s gauge\n%s %d\n", name, name, g.Value())
		}
		for name, h := range r.histograms {
			fmt.Fprintf(w, "# TYPE %s summary\n", name)
			fmt.Fprintf(w, "%s{quantile=\"0.5\"} %.3f\n", name, h.Quantile(0.5))
			fmt.Fprintf(w, "%s{quantile=\"0.95\"} %.3f\n", name, h.Quantile(0.95))
			fmt.Fprintf(w, "%s{quantile=\"0.99\"} %.3f\n", name, h.Quantile(0.99))
			h.mu.Lock()
			fmt.Fprintf(w, "%s_sum %.3f\n%s_count %d\n", name, h.sum, name, h.count)
			h.mu.Unlock()
		}
	})
}

// Serve starts an HTTP server exposing /metrics on addr. Non-blocking.
func (r *Registry) Serve(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", r.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { _ = http.ListenAndServe(addr, mux) }()
}
