package base64util

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestEstimate_Empty(t *testing.T) {
	if got := Estimate(""); got != 0 {
		t.Errorf("expected 0, got %d", got)
	}
	if got := Estimate("   "); got != 0 {
		t.Errorf("expected 0 for whitespace, got %d", got)
	}
}

func TestEstimate_NoPadding(t *testing.T) {
	// "AAAA" = 4 base64 chars -> 3 decoded bytes.
	if got := Estimate("AAAA"); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestEstimate_SinglePadding(t *testing.T) {
	// "AAA=" = 4 chars, 1 padding -> 2 decoded bytes.
	if got := Estimate("AAA="); got != 2 {
		t.Errorf("expected 2, got %d", got)
	}
}

func TestEstimate_DoublePadding(t *testing.T) {
	// "AA==" = 4 chars, 2 padding -> 1 decoded byte.
	if got := Estimate("AA=="); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

func TestEstimate_WithWhitespace(t *testing.T) {
	if got := Estimate("  AA AA  "); got != Estimate("AAAA") {
		t.Errorf("expected same as AAAA, got %d", got)
	}
	if got := Estimate("A A\nA\tA"); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestEstimate_URLSafe(t *testing.T) {
	// URL-safe chars don't affect size estimation (same count).
	if got := Estimate("ab-_"); got != 3 {
		t.Errorf("expected 3, got %d", got)
	}
}

func TestCanonicalize_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"AAAA", "AAAA"},
		{"AA==", "AA=="},
		{" A A A A ", "AAAA"},
		{"AQID\nBAUG", "AQIDBAUG"},
	}
	for _, tt := range tests {
		got, err := Canonicalize(tt.input)
		if err != nil {
			t.Errorf("Canonicalize(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Canonicalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCanonicalize_URLSafe(t *testing.T) {
	// URL-safe uses '-' for '+' and '_' for '/'.
	got := testutil.Must(Canonicalize("ab-_"))
	if got != "ab+/" {
		t.Errorf("expected ab+/, got %s", got)
	}
	// Mixed standard and URL-safe.
	got = testutil.Must(Canonicalize("ab+_"))
	if got != "ab+/" {
		t.Errorf("expected ab+/, got %s", got)
	}
}

func TestCanonicalize_InvalidLength(t *testing.T) {
	if _, err := Canonicalize("AAA"); err == nil {
		t.Error("expected error for non-multiple-of-4")
	}
}

func TestCanonicalize_InvalidChars(t *testing.T) {
	if _, err := Canonicalize("A@A="); err == nil {
		t.Error("expected error for invalid chars")
	}
}

func TestCanonicalize_Empty(t *testing.T) {
	if _, err := Canonicalize(""); err == nil {
		t.Error("expected error for empty input")
	}
	if _, err := Canonicalize("   \t\n  "); err == nil {
		t.Error("expected error for whitespace-only")
	}
}

func TestCanonicalize_TriplePadding(t *testing.T) {
	if _, err := Canonicalize("A==="); err == nil {
		t.Error("expected error for triple padding")
	}
}

// --- Rust parity: URL-safe estimate unchanged ---

func TestEstimate_MixedURLSafe(t *testing.T) {
	// Standard and URL-safe chars have same byte count.
	if Estimate("ab+/") != Estimate("ab-_") {
		t.Error("expected same estimate for standard and URL-safe")
	}
}

// --- Rust parity: whitespace variants ---

func TestEstimate_TabsAndNewlines(t *testing.T) {
	if got := Estimate("\tAA\n==\r"); got != 1 {
		t.Errorf("expected 1, got %d", got)
	}
}

// --- Rust parity: canonicalize mixed URL-safe ---

func TestCanonicalize_MixedStandardAndURLSafe(t *testing.T) {
	got := testutil.Must(Canonicalize("ab+_"))
	if got != "ab+/" {
		t.Errorf("expected ab+/, got %s", got)
	}
}

// --- Rust parity: padding in middle (invalid) ---

func TestCanonicalize_PaddingInMiddle(t *testing.T) {
	if _, err := Canonicalize("A=AA"); err == nil {
		t.Error("expected error for padding in middle")
	}
}
