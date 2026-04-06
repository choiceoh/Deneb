package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestHistogramQuantiles(t *testing.T) {
	h := NewHistogram("test_duration", "test", []float64{0.1, 0.5, 1, 5, 10}, "method")

	// Record observations: 10 values centered around 0.3s.
	for i := 0; i < 10; i++ {
		h.Observe(0.3, "foo")
	}
	// Add 1 slow outlier at 8s.
	h.Observe(8.0, "foo")

	qs := h.Quantiles([]float64{0.50, 0.95, 0.99}, "foo")
	if qs == nil {
		t.Fatal("Quantiles returned nil")
	}

	// p50 should be well under 1s (most values are 0.3).
	if qs[0.50] >= 1.0 {
		t.Errorf("p50 = %g, want < 1.0", qs[0.50])
	}

	// p99 should be high (the outlier is at 8s).
	if qs[0.99] < 1.0 {
		t.Errorf("p99 = %g, want >= 1.0", qs[0.99])
	}
}

func TestHistogramQuantilesEmpty(t *testing.T) {
	h := NewHistogram("test_empty", "test", []float64{1, 5, 10}, "method")
	qs := h.Quantiles([]float64{0.50}, "nonexistent")
	if qs != nil {
		t.Errorf("expected nil for missing series, got %v", qs)
	}
}

func TestHistogramWriteQuantiles(t *testing.T) {
	h := NewHistogram("test_duration", "test", []float64{0.1, 0.5, 1, 5, 10}, "method")
	h.Observe(0.3, "foo")

	var buf bytes.Buffer
	h.writeQuantilesTo(&buf)
	output := buf.String()

	if !strings.Contains(output, "test_duration_p50") {
		t.Error("missing p50 in output")
	}
	if !strings.Contains(output, "test_duration_p95") {
		t.Error("missing p95 in output")
	}
	if !strings.Contains(output, "test_duration_p99") {
		t.Error("missing p99 in output")
	}
	if !strings.Contains(output, `method="foo"`) {
		t.Error("missing method label in output")
	}
}

func TestWorkerPoolGaugesExist(t *testing.T) {
	// Verify the global pool gauges are initialized.
	if WorkerPoolActive == nil {
		t.Error("WorkerPoolActive is nil")
	}
	if WorkerPoolQueued == nil {
		t.Error("WorkerPoolQueued is nil")
	}
	if WorkerPoolCapacity == nil {
		t.Error("WorkerPoolCapacity is nil")
	}
}

func TestRPCRequestsTotalHasCodeLabel(t *testing.T) {
	// Verify the counter was created with 3 labels.
	if len(RPCRequestsTotal.labels) != 3 {
		t.Errorf("RPCRequestsTotal has %d labels, want 3 (method, status, code)", len(RPCRequestsTotal.labels))
	}
	if RPCRequestsTotal.labels[2] != "code" {
		t.Errorf("third label = %q, want %q", RPCRequestsTotal.labels[2], "code")
	}

	// Verify incrementing with 3 label values works.
	RPCRequestsTotal.Inc("test.method", "error", "NOT_FOUND")
	snap := RPCRequestsTotal.Snapshot()
	key := "test.method\x00error\x00NOT_FOUND"
	if snap[key] != 1 {
		t.Errorf("counter value = %d, want 1", snap[key])
	}
}
