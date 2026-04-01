package chat

import "testing"

func TestCompactionCircuitBreaker(t *testing.T) {
	t.Run("not tripped initially", func(t *testing.T) {
		cb := NewCompactionCircuitBreaker()
		if cb.IsTripped() {
			t.Error("should not be tripped initially")
		}
		if cb.ConsecutiveFailures() != 0 {
			t.Errorf("failures = %d, want 0", cb.ConsecutiveFailures())
		}
	})

	t.Run("trips after max failures", func(t *testing.T) {
		cb := NewCompactionCircuitBreaker()
		for i := 0; i < maxConsecutiveCompactionFailures-1; i++ {
			if tripped := cb.RecordFailure(); tripped {
				t.Errorf("tripped too early at failure %d", i+1)
			}
		}
		if tripped := cb.RecordFailure(); !tripped {
			t.Error("should trip at max failures")
		}
		if !cb.IsTripped() {
			t.Error("IsTripped should return true")
		}
	})

	t.Run("success resets", func(t *testing.T) {
		cb := NewCompactionCircuitBreaker()
		for i := 0; i < maxConsecutiveCompactionFailures; i++ {
			cb.RecordFailure()
		}
		if !cb.IsTripped() {
			t.Error("should be tripped")
		}
		cb.RecordSuccess()
		if cb.IsTripped() {
			t.Error("should reset after success")
		}
		if cb.ConsecutiveFailures() != 0 {
			t.Errorf("failures = %d, want 0", cb.ConsecutiveFailures())
		}
	})

	t.Run("partial failures then success", func(t *testing.T) {
		cb := NewCompactionCircuitBreaker()
		cb.RecordFailure()
		cb.RecordFailure()
		cb.RecordSuccess()
		if cb.ConsecutiveFailures() != 0 {
			t.Errorf("failures = %d after success", cb.ConsecutiveFailures())
		}
		// Need full maxConsecutiveCompactionFailures again to trip.
		for i := 0; i < maxConsecutiveCompactionFailures-1; i++ {
			cb.RecordFailure()
		}
		if cb.IsTripped() {
			t.Error("should not trip yet after reset")
		}
	})

	t.Run("reset forces closed state", func(t *testing.T) {
		cb := NewCompactionCircuitBreaker()
		for i := 0; i < maxConsecutiveCompactionFailures; i++ {
			cb.RecordFailure()
		}
		cb.Reset()
		if cb.IsTripped() {
			t.Error("should not be tripped after reset")
		}
	})
}
