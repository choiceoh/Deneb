package metrics

import (
	"testing"
)

func BenchmarkCounterInc(b *testing.B) {
	c := NewCounter()
	b.ResetTimer()
	for range b.N {
		c.Inc("rpc.echo", "ok")
	}
}

func BenchmarkCounterSnapshot(b *testing.B) {
	c := NewCounter()
	for range 10 {
		c.Inc("method_a", "ok")
		c.Inc("method_b", "error")
	}
	b.ResetTimer()
	for range b.N {
		_ = c.Snapshot()
	}
}
