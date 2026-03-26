package aurora

import (
	"strings"
	"testing"
)

func TestDeterministicFallback_ShortText(t *testing.T) {
	text := "Short text that fits."
	got := deterministicFallback(text)
	if got != text {
		t.Errorf("expected unchanged text, got %q", got)
	}
}

func TestDeterministicFallback_ExactBoundary(t *testing.T) {
	text := strings.Repeat("a", 512*4)
	got := deterministicFallback(text)
	if got != text {
		t.Error("expected unchanged text at exact boundary")
	}
}

func TestDeterministicFallback_LongText(t *testing.T) {
	text := strings.Repeat("x", 512*4+100)
	got := deterministicFallback(text)

	if !strings.Contains(got, "...[truncated]...") {
		t.Error("expected truncation marker")
	}

	half := 512 * 4 / 2
	// First half chars should be from the start.
	if got[:half] != text[:half] {
		t.Error("expected first half to match start of text")
	}
	// Last half chars should be from the end.
	if got[len(got)-half:] != text[len(text)-half:] {
		t.Error("expected last half to match end of text")
	}
}

func TestDeterministicFallback_Empty(t *testing.T) {
	got := deterministicFallback("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestSafeUint32_Nil(t *testing.T) {
	if safeUint32(nil) != 0 {
		t.Error("expected 0 for nil pointer")
	}
}

func TestSafeUint32_Value(t *testing.T) {
	v := uint32(42)
	if safeUint32(&v) != 42 {
		t.Errorf("expected 42, got %d", safeUint32(&v))
	}
}
