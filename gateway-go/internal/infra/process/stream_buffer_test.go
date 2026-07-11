package process

import (
	"strings"
	"testing"
)

func TestStreamBuffer_KeepsTailAndCountsDropped(t *testing.T) {
	sb := NewStreamBuffer(10)

	if _, err := sb.Write([]byte("0123456789")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := sb.Dropped(); got != 0 {
		t.Fatalf("dropped after exact-capacity write = %d, want 0", got)
	}

	if _, err := sb.Write([]byte("abcde")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := sb.Snapshot(); got != "56789abcde" {
		t.Errorf("snapshot = %q, want tail %q", got, "56789abcde")
	}
	if got := sb.Dropped(); got != 5 {
		t.Errorf("dropped = %d, want 5", got)
	}

	// Drops accumulate across writes — the counter is total head loss.
	if _, err := sb.Write([]byte(strings.Repeat("x", 7))); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := sb.Dropped(); got != 12 {
		t.Errorf("dropped = %d, want 12", got)
	}
}
