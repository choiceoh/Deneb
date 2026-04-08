package metrics

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestCounterInc(t *testing.T) {
	c := NewCounter("test_total", "test counter", "method", "status")
	c.Inc("foo", "ok")
	c.Inc("foo", "ok")
	c.Inc("foo", "error")

	var buf bytes.Buffer
	c.writeTo(&buf)
	out := buf.String()

	if !strings.Contains(out, `test_total{method="foo",status="ok"} 2`) {
		t.Errorf("expected foo/ok=2, got:\n%s", out)
	}
	if !strings.Contains(out, `test_total{method="foo",status="error"} 1`) {
		t.Errorf("expected foo/error=1, got:\n%s", out)
	}
}

func TestCounterAdd(t *testing.T) {
	c := NewCounter("tokens_total", "token counter", "direction")
	c.Add(100, "input")
	c.Add(50, "output")
	c.Add(200, "input")

	var buf bytes.Buffer
	c.writeTo(&buf)
	out := buf.String()

	if !strings.Contains(out, `tokens_total{direction="input"} 300`) {
		t.Errorf("expected input=300, got:\n%s", out)
	}
	if !strings.Contains(out, `tokens_total{direction="output"} 50`) {
		t.Errorf("expected output=50, got:\n%s", out)
	}
}

func TestGauge(t *testing.T) {
	g := NewGauge("active_sessions", "active sessions")
	g.Inc()
	g.Inc()
	g.Dec()

	var buf bytes.Buffer
	g.writeTo(&buf)
	out := buf.String()

	if !strings.Contains(out, "active_sessions 1") {
		t.Errorf("expected 1, got:\n%s", out)
	}
}

func TestHistogramObserve(t *testing.T) {
	h := NewHistogram("duration_seconds", "duration", []float64{0.1, 0.5, 1.0}, "method")
	h.Observe(0.05, "rpc")
	h.Observe(0.3, "rpc")
	h.Observe(2.0, "rpc")

	var buf bytes.Buffer
	h.writeTo(&buf)
	out := buf.String()

	// 0.05 <= 0.1, so bucket 0.1 should have 1
	if !strings.Contains(out, `le="0.1"} 1`) {
		t.Errorf("expected le=0.1 count 1, got:\n%s", out)
	}
	// 0.05 and 0.3 <= 0.5, so bucket 0.5 should have 2
	if !strings.Contains(out, `le="0.5"} 2`) {
		t.Errorf("expected le=0.5 count 2, got:\n%s", out)
	}
	// All 3 <= +Inf
	if !strings.Contains(out, `le="+Inf"} 3`) {
		t.Errorf("expected +Inf count 3, got:\n%s", out)
	}
	if !strings.Contains(out, `duration_seconds_count{method="rpc"} 3`) {
		t.Errorf("expected count 3, got:\n%s", out)
	}
}

func TestHistogramObserveDuration(t *testing.T) {
	h := NewHistogram("test_duration", "test", []float64{0.001, 0.01, 0.1}, "op")
	start := time.Now()
	// Observe immediately — should be < 1ms.
	h.ObserveDuration(start, "fast")

	var buf bytes.Buffer
	h.writeTo(&buf)
	out := buf.String()

	if !strings.Contains(out, `test_duration_count{op="fast"} 1`) {
		t.Errorf("expected count 1, got:\n%s", out)
	}
}

