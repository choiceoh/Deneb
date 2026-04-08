package httpretry

import (
	"testing"
	"time"
)

func TestBackoff_Delay(t *testing.T) {
	b := Backoff{Base: 1 * time.Second, Max: 60 * time.Second}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},  // 1s * 2^0
		{2, 2 * time.Second},  // 1s * 2^1
		{3, 4 * time.Second},  // 1s * 2^2
		{4, 8 * time.Second},  // 1s * 2^3
		{7, 60 * time.Second}, // capped at max
	}
	for _, tt := range tests {
		got := b.Delay(tt.attempt)
		if got != tt.want {
			t.Errorf("Delay(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestBackoff_Jitter(t *testing.T) {
	b := Backoff{Base: 1 * time.Second, Max: 60 * time.Second, Jitter: 0.25}

	// With 25% jitter, attempt 1 should be in [1s, 1.25s).
	seen := make(map[time.Duration]bool)
	for range 100 {
		d := b.Delay(1)
		if d < 1*time.Second || d >= 1250*time.Millisecond {
			t.Fatalf("Delay(1) with 0.25 jitter = %v, want [1s, 1.25s)", d)
		}
		seen[d] = true
	}
	// Should produce more than one distinct value (not deterministic).
	if len(seen) < 2 {
		t.Error("jitter produced no variance over 100 samples")
	}
}

