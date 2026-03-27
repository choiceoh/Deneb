// Package metrics provides lightweight instrumentation for the Deneb gateway.
//
// All metrics use sync/atomic for lock-free, concurrent-safe recording.
// The /metrics endpoint outputs Prometheus-compatible text format so it can
// be scraped by Prometheus or queried with curl.
//
// No external dependencies — stdlib only.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Counter is a monotonically increasing counter keyed by label values.
type Counter struct {
	mu     sync.RWMutex
	values map[string]*atomic.Int64
	name   string
	help   string
	labels []string
}

// NewCounter creates a new labeled counter.
func NewCounter(name, help string, labels ...string) *Counter {
	return &Counter{
		values: make(map[string]*atomic.Int64),
		name:   name,
		help:   help,
		labels: labels,
	}
}

// Inc increments the counter for the given label values.
func (c *Counter) Inc(labelValues ...string) {
	key := strings.Join(labelValues, "\x00")
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		v.Add(1)
		return
	}
	c.mu.Lock()
	if v, ok = c.values[key]; ok {
		c.mu.Unlock()
		v.Add(1)
		return
	}
	v = &atomic.Int64{}
	v.Store(1)
	c.values[key] = v
	c.mu.Unlock()
}

// Add adds delta to the counter for the given label values.
func (c *Counter) Add(delta int64, labelValues ...string) {
	key := strings.Join(labelValues, "\x00")
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		v.Add(delta)
		return
	}
	c.mu.Lock()
	if v, ok = c.values[key]; ok {
		c.mu.Unlock()
		v.Add(delta)
		return
	}
	v = &atomic.Int64{}
	v.Store(delta)
	c.values[key] = v
	c.mu.Unlock()
}

// writeTo writes the counter in Prometheus text format.
func (c *Counter) writeTo(w io.Writer) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.values) == 0 {
		return
	}
	fmt.Fprintf(w, "# HELP %s %s\n", c.name, c.help)
	fmt.Fprintf(w, "# TYPE %s counter\n", c.name)
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		labelStr := formatLabels(c.labels, key)
		fmt.Fprintf(w, "%s%s %d\n", c.name, labelStr, c.values[key].Load())
	}
}

// Gauge is a value that can go up and down.
type Gauge struct {
	value atomic.Int64
	name  string
	help  string
}

// NewGauge creates a new gauge.
func NewGauge(name, help string) *Gauge {
	return &Gauge{name: name, help: help}
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() { g.value.Add(1) }

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() { g.value.Add(-1) }

// Set sets the gauge to the given value.
func (g *Gauge) Set(v int64) { g.value.Store(v) }

// writeTo writes the gauge in Prometheus text format.
func (g *Gauge) writeTo(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n", g.name, g.help)
	fmt.Fprintf(w, "# TYPE %s gauge\n", g.name)
	fmt.Fprintf(w, "%s %d\n", g.name, g.value.Load())
}

// Histogram tracks the distribution of observed values using fixed buckets.
// Stores count and sum for Prometheus-compatible output.
type Histogram struct {
	mu      sync.RWMutex
	series  map[string]*histogramData
	name    string
	help    string
	labels  []string
	buckets []float64
}

type histogramData struct {
	bucketCounts []atomic.Int64
	count        atomic.Int64
	// sumMicros stores sum × 1e6 as int64 for atomic operations.
	sumMicros atomic.Int64
}

// NewHistogram creates a new labeled histogram with the given buckets.
func NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	return &Histogram{
		series:  make(map[string]*histogramData),
		name:    name,
		help:    help,
		labels:  labels,
		buckets: buckets,
	}
}

func (h *Histogram) getOrCreate(key string) *histogramData {
	h.mu.RLock()
	d, ok := h.series[key]
	h.mu.RUnlock()
	if ok {
		return d
	}
	h.mu.Lock()
	if d, ok = h.series[key]; ok {
		h.mu.Unlock()
		return d
	}
	d = &histogramData{
		bucketCounts: make([]atomic.Int64, len(h.buckets)),
	}
	h.series[key] = d
	h.mu.Unlock()
	return d
}

// Observe records a value in the histogram for the given label values.
func (h *Histogram) Observe(value float64, labelValues ...string) {
	key := strings.Join(labelValues, "\x00")
	d := h.getOrCreate(key)
	d.count.Add(1)
	d.sumMicros.Add(int64(value * 1e6))
	for i, bound := range h.buckets {
		if value <= bound {
			d.bucketCounts[i].Add(1)
			break
		}
	}
}

// ObserveDuration records a duration in seconds for the given label values.
func (h *Histogram) ObserveDuration(start time.Time, labelValues ...string) {
	h.Observe(time.Since(start).Seconds(), labelValues...)
}

// writeTo writes the histogram in Prometheus text format.
func (h *Histogram) writeTo(w io.Writer) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.series) == 0 {
		return
	}
	fmt.Fprintf(w, "# HELP %s %s\n", h.name, h.help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", h.name)
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		d := h.series[key]
		labelStr := formatLabels(h.labels, key)
		cumulative := int64(0)
		for i, bound := range h.buckets {
			cumulative += d.bucketCounts[i].Load()
			le := fmt.Sprintf("%g", bound)
			if labelStr == "" {
				fmt.Fprintf(w, "%s_bucket{le=\"%s\"} %d\n", h.name, le, cumulative)
			} else {
				// Insert le into existing labels.
				fmt.Fprintf(w, "%s_bucket{%s,le=\"%s\"} %d\n", h.name, labelStr[1:len(labelStr)-1], le, cumulative)
			}
		}
		count := d.count.Load()
		if labelStr == "" {
			fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %d\n", h.name, count)
		} else {
			fmt.Fprintf(w, "%s_bucket{%s,le=\"+Inf\"} %d\n", h.name, labelStr[1:len(labelStr)-1], count)
		}
		sum := float64(d.sumMicros.Load()) / 1e6
		fmt.Fprintf(w, "%s_sum%s %g\n", h.name, labelStr, sum)
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, labelStr, count)
	}
}

// formatLabels builds a Prometheus label string like {method="foo",status="ok"}.
func formatLabels(names []string, key string) string {
	if len(names) == 0 {
		return ""
	}
	parts := strings.Split(key, "\x00")
	var b strings.Builder
	b.WriteByte('{')
	for i, name := range names {
		if i > 0 {
			b.WriteByte(',')
		}
		val := ""
		if i < len(parts) {
			val = parts[i]
		}
		fmt.Fprintf(&b, "%s=%q", name, val)
	}
	b.WriteByte('}')
	return b.String()
}

// --- Global metrics instances ---

// RPC metrics.
var (
	RPCRequestsTotal = NewCounter("deneb_rpc_requests_total", "Total RPC requests by method and status.", "method", "status")
	RPCDuration      = NewHistogram("deneb_rpc_duration_seconds", "RPC request duration in seconds.",
		[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, "method")
)

// LLM metrics.
var (
	LLMRequestDuration = NewHistogram("deneb_llm_request_duration_seconds", "LLM API request duration in seconds.",
		[]float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120}, "provider", "model")
	LLMTokensTotal = NewCounter("deneb_llm_tokens_total", "Total LLM tokens by direction and model.", "direction", "model")
)

// Session and connection metrics.
var (
	ActiveSessions   = NewGauge("deneb_active_sessions", "Number of currently active sessions.")
	WebSocketClients = NewGauge("deneb_websocket_clients", "Number of connected WebSocket clients.")
)

// allMetrics is the ordered list of all metric writers for the /metrics handler.
var allMetrics = []interface{ writeTo(io.Writer) }{
	RPCRequestsTotal,
	RPCDuration,
	LLMRequestDuration,
	LLMTokensTotal,
	ActiveSessions,
	WebSocketClients,
}

// WriteMetrics writes all metrics in Prometheus text exposition format.
func WriteMetrics(w io.Writer) {
	for _, m := range allMetrics {
		m.writeTo(w)
	}
}
