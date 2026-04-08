package base64util

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)


func TestEstimate_NoPadding(t *testing.T) {
	// "AAAA" = 4 base64 chars -> 3 decoded bytes.
	if got := Estimate("AAAA"); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

func TestEstimate_SinglePadding(t *testing.T) {
	// "AAA=" = 4 chars, 1 padding -> 2 decoded bytes.
	if got := Estimate("AAA="); got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestEstimate_DoublePadding(t *testing.T) {
	// "AA==" = 4 chars, 2 padding -> 1 decoded byte.
	if got := Estimate("AA=="); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestEstimate_WithWhitespace(t *testing.T) {
	if got := Estimate("  AA AA  "); got != Estimate("AAAA") {
		t.Errorf("got %d, want same as AAAA", got)
	}
	if got := Estimate("A A\nA\tA"); got != 3 {
		t.Errorf("got %d, want 3", got)
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
		t.Errorf("got %s, want ab+/", got)
	}
	// Mixed standard and URL-safe.
	got = testutil.Must(Canonicalize("ab+_"))
	if got != "ab+/" {
		t.Errorf("got %s, want ab+/", got)
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


func TestCanonicalize_TriplePadding(t *testing.T) {
	if _, err := Canonicalize("A==="); err == nil {
		t.Error("expected error for triple padding")
	}
}

// --- Rust parity: URL-safe estimate unchanged ---


// --- Rust parity: whitespace variants ---


// --- Rust parity: canonicalize mixed URL-safe ---


// --- Rust parity: padding in middle (invalid) ---

func TestCanonicalize_PaddingInMiddle(t *testing.T) {
	if _, err := Canonicalize("A=AA"); err == nil {
		t.Error("expected error for padding in middle")
	}
}
