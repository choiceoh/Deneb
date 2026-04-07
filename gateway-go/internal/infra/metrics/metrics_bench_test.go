package metrics

import (
	"bytes"
	"testing"
)

func BenchmarkCounterInc(b *testing.B) {
	c := NewCounter("bench_total", "bench", "method", "status")
	b.ResetTimer()
	for range b.N {
		c.Inc("rpc.echo", "ok")
	}
}

func BenchmarkHistogramObserve(b *testing.B) {
	h := NewHistogram("bench_duration", "bench",
		[]float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		"method")
	b.ResetTimer()
	for range b.N {
		h.Observe(0.042, "rpc.echo")
	}
}

func BenchmarkGaugeIncDec(b *testing.B) {
	g := NewGauge("bench_gauge", "bench")
	b.ResetTimer()
	for range b.N {
		g.Inc()
		g.Dec()
	}
}

func BenchmarkWriteMetrics(b *testing.B) {
	// Pre-populate some data.
	c := NewCounter("bench_counter", "bench", "method")
	for range 10 {
		c.Inc("method_a")
		c.Inc("method_b")
	}

	var buf bytes.Buffer
	b.ResetTimer()
	for range b.N {
		buf.Reset()
		c.writeTo(&buf)
	}
}
