package server

import "testing"

// TestTruncateForDedup verifies that truncateForDedup correctly clips strings
// used as deduplication keys. The function is byte-based (not rune-based) by
// design — dedup keys only need to compare equal for identical byte prefixes.
func TestTruncateForDedup(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"empty string", "", 10, ""},
		{"shorter than max", "hello", 10, "hello"},
		{"exact max length", "hello", 5, "hello"},
		{"longer than max", "hello world", 5, "hello"},
		{"zero max clips all", "hello", 0, ""},
		{"single char", "x", 1, "x"},
		{"single char clipped", "xy", 1, "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForDedup(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForDedup(%q, %d) = %q, want %q",
					tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestTruncateForDedup_Idempotent verifies that applying truncation twice
// yields the same result as applying it once.
func TestTruncateForDedup_Idempotent(t *testing.T) {
	input := "hello world long string"
	maxLen := 8
	once := truncateForDedup(input, maxLen)
	twice := truncateForDedup(once, maxLen)
	if once != twice {
		t.Errorf("idempotency violation: once=%q, twice=%q", once, twice)
	}
}
