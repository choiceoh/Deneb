package session

import (
	"fmt"
	"testing"
)

// BenchmarkManagerCreate measures session creation throughput.
func BenchmarkManagerCreate(b *testing.B) {
	m := NewManager()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Create(fmt.Sprintf("sess-%d", i), KindDirect)
	}
}

// BenchmarkManagerGet measures single session lookup (hot path for RPC dispatch).
func BenchmarkManagerGet(b *testing.B) {
	m := NewManager()
	for i := 0; i < 100; i++ {
		m.Create(fmt.Sprintf("sess-%d", i), KindDirect)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get(fmt.Sprintf("sess-%d", i%100))
	}
}

// BenchmarkManagerGetMiss measures lookup miss (negative path).
func BenchmarkManagerGetMiss(b *testing.B) {
	m := NewManager()
	for i := 0; i < 100; i++ {
		m.Create(fmt.Sprintf("sess-%d", i), KindDirect)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Get("nonexistent-key")
	}
}

// BenchmarkManagerList measures listing all sessions (used by session.list RPC).
func BenchmarkManagerList(b *testing.B) {
	for _, n := range []int{10, 100, 500} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			m := NewManager()
			for i := 0; i < n; i++ {
				m.Create(fmt.Sprintf("sess-%d", i), KindDirect)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.List()
			}
		})
	}
}

// BenchmarkManagerSet measures session update with status transition validation.
func BenchmarkManagerSet(b *testing.B) {
	m := NewManager()
	m.Create("sess-0", KindDirect)
	s := &Session{Key: "sess-0", Kind: KindDirect, Status: StatusRunning}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.Set(s)
	}
}

// BenchmarkManagerConcurrentGetSet measures contended read/write mix
// (typical pattern: multiple RPC handlers reading while chat pipeline writes).
func BenchmarkManagerConcurrentGetSet(b *testing.B) {
	m := NewManager()
	for i := 0; i < 50; i++ {
		m.Create(fmt.Sprintf("sess-%d", i), KindDirect)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("sess-%d", i%50)
			if i%4 == 0 {
				_ = m.Set(&Session{Key: key, Kind: KindDirect, Status: StatusRunning})
			} else {
				m.Get(key)
			}
			i++
		}
	})
}

// BenchmarkKeyCacheGet measures LRU cache hit performance (run ID → session key).
func BenchmarkKeyCacheGet(b *testing.B) {
	c := NewKeyCache()
	for i := 0; i < 200; i++ {
		c.Put(fmt.Sprintf("run-%d", i), fmt.Sprintf("sess-%d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(fmt.Sprintf("run-%d", i%200))
	}
}

// BenchmarkKeyCachePut measures LRU cache insert with eviction pressure.
func BenchmarkKeyCachePut(b *testing.B) {
	c := NewKeyCache()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Put(fmt.Sprintf("run-%d", i), fmt.Sprintf("sess-%d", i))
	}
}

// BenchmarkKeyCacheConcurrent measures contended cache access.
func BenchmarkKeyCacheConcurrent(b *testing.B) {
	c := NewKeyCache()
	for i := 0; i < 200; i++ {
		c.Put(fmt.Sprintf("run-%d", i), fmt.Sprintf("sess-%d", i))
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("run-%d", i%200)
			if i%5 == 0 {
				c.Put(key, fmt.Sprintf("sess-%d", i))
			} else {
				c.Get(key)
			}
			i++
		}
	})
}

// BenchmarkApplyLifecycleEvent measures the full lifecycle event path
// (create-if-missing + status transition + event emission).
func BenchmarkApplyLifecycleEvent(b *testing.B) {
	m := NewManager()
	// Subscribe to drain events so they don't block.
	m.EventBusRef().Subscribe(func(Event) {})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("sess-%d", i)
		m.ApplyLifecycleEvent(key, LifecycleEvent{Phase: PhaseStart, Ts: int64(i * 1000)})
		m.ApplyLifecycleEvent(key, LifecycleEvent{Phase: PhaseEnd, Ts: int64(i*1000 + 500)})
	}
}

// BenchmarkEventBusEmit measures event emission overhead with multiple subscribers.
func BenchmarkEventBusEmit(b *testing.B) {
	bus := NewEventBus()
	for i := 0; i < 5; i++ {
		bus.Subscribe(func(Event) {})
	}
	evt := Event{Kind: EventStatusChanged, Key: "sess-1", OldStatus: StatusRunning, NewStatus: StatusDone}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Emit(evt)
	}
}
