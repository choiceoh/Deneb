package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestHistogramQuantiles(t *testing.T) {
	h := NewHistogram("test_duration", "test", []float64{0.1, 0.5, 1, 5, 10}, "method")

	// Record observations: 10 values centered around 0.3s.
	for range 10 {
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


