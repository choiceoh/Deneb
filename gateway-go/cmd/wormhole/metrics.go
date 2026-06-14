package main

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"
)

// metrics is wormhole's per-request observability — the thing that was missing
// when wormhole became Deneb's model hot path: it only logged errors, so
// successful traffic was invisible. Counters are kept per client-facing model
// name and exposed at GET /metrics in Prometheus text format (the same shape vLLM
// and SparkFleet expose), so "what is flowing, how fast, how often it fails" is
// answerable without grepping logs or probing the upstream.
type metrics struct {
	mu      sync.Mutex // guards the maps/counters; held only briefly (no I/O under lock)
	total   int64
	errors  int64
	byModel map[string]*modelStat
}

type modelStat struct {
	requests int64
	errors   int64
	sumMs    int64 // cumulative latency; avg = sumMs/requests
}

func newMetrics() *metrics { return &metrics{byModel: map[string]*modelStat{}} }

// record folds one finished request into the counters. status 0 (upstream
// unreachable) and >=400 count as errors.
func (m *metrics) record(model string, status int, d time.Duration) {
	if model == "" {
		model = "(none)"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.total++
	st := m.byModel[model]
	if st == nil {
		st = &modelStat{}
		m.byModel[model] = st
	}
	st.requests++
	st.sumMs += d.Milliseconds()
	if status == 0 || status >= 400 {
		m.errors++
		st.errors++
	}
}

// writePrometheus emits the counters in Prometheus text format. It snapshots
// under the lock then writes without it, so a slow client can't stall recorders.
func (m *metrics) writePrometheus(w io.Writer) {
	m.mu.Lock()
	total, errors := m.total, m.errors
	type row struct {
		model string
		st    modelStat
	}
	rows := make([]row, 0, len(m.byModel))
	for k, v := range m.byModel {
		rows = append(rows, row{k, *v})
	}
	m.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].model < rows[j].model }) // stable output

	fmt.Fprint(w, "# HELP wormhole_requests_total Total requests handled.\n# TYPE wormhole_requests_total counter\n")
	fmt.Fprintf(w, "wormhole_requests_total %d\n", total)
	fmt.Fprint(w, "# HELP wormhole_request_errors_total Requests that failed (status 0 or >=400).\n# TYPE wormhole_request_errors_total counter\n")
	fmt.Fprintf(w, "wormhole_request_errors_total %d\n", errors)
	fmt.Fprint(w, "# HELP wormhole_model_requests_total Requests per model.\n# TYPE wormhole_model_requests_total counter\n")
	for _, r := range rows {
		fmt.Fprintf(w, "wormhole_model_requests_total{model=%q} %d\n", r.model, r.st.requests)
	}
	fmt.Fprint(w, "# HELP wormhole_model_errors_total Failed requests per model.\n# TYPE wormhole_model_errors_total counter\n")
	for _, r := range rows {
		fmt.Fprintf(w, "wormhole_model_errors_total{model=%q} %d\n", r.model, r.st.errors)
	}
	fmt.Fprint(w, "# HELP wormhole_model_latency_ms_sum Cumulative request latency per model; divide by requests for the average.\n# TYPE wormhole_model_latency_ms_sum counter\n")
	for _, r := range rows {
		fmt.Fprintf(w, "wormhole_model_latency_ms_sum{model=%q} %d\n", r.model, r.st.sumMs)
	}
}

// metricsHandler serves GET /metrics in Prometheus text format. Token-gated like
// the model endpoints (it enumerates model names + traffic); open when no token
// is configured (loopback dev), same as /v1/models and /status.
func (rt *router) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if !rt.authed(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	rt.metrics.writePrometheus(w)
}

// statusWriter wraps a ResponseWriter to capture the status code (and preserve
// streaming via Flush), so serve() can record the final status of any path —
// early 4xx, a forwarded upstream status, or an auto-routing 5xx — from one place
// without threading it through every handler.
type statusWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusWriter) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK // a body write with no explicit WriteHeader means 200
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer so SSE streaming keeps flushing through
// the wrapper (streamResponse type-asserts http.Flusher).
func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
