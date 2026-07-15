package web

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// buildEUCKRIsh returns a byte string with `n` invalid-UTF-8 byte pairs
// (EUC-KR '가' = 0xB0 0xA1), which strings.ToLower would each expand into a
// 3-byte U+FFFD — the exact condition that made a lower-derived index exceed
// len(html) and panic in the strip helpers.
func buildEUCKRIsh(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(0xB0)
		b.WriteByte(0xA1)
	}
	return b.String()
}

func TestStripNoiseElementsOnNonUTF8PageDoesNotPanicAndStripsNoise(t *testing.T) {
	// Non-UTF-8 (EUC-KR-ish) body large enough that strings.ToLower would grow
	// the string well past its byte length, then a noise <footer> block and
	// real content after it.
	junk := buildEUCKRIsh(2000) // 4000 invalid bytes -> +8000 under strings.ToLower
	html := junk +
		"<DIV>keep-this-content</DIV>" +
		"<FOOTER><NAV>noise-menu</NAV></FOOTER>" +
		"trailing-visible"

	if utf8.ValidString(html) {
		t.Fatal("test precondition: input must contain invalid UTF-8")
	}

	// Before the asciiLower fix this panicked with
	// "slice bounds out of range [:N] with length M".
	out := StripNoiseElements(html)

	if strings.Contains(strings.ToLower(out), "noise-menu") {
		t.Errorf("footer/nav noise block was not stripped: %q", tail(out))
	}
	if !strings.Contains(out, "keep-this-content") {
		t.Errorf("non-noise <div> content was dropped: %q", tail(out))
	}
	if !strings.Contains(out, "trailing-visible") {
		t.Errorf("trailing content after the stripped block was lost: %q", tail(out))
	}
}

func TestAsciiLowerIsLengthPreservingAndFoldsASCIIOnly(t *testing.T) {
	// Invalid bytes + mixed-case ASCII tag tokens.
	in := "\xB0\xA1<SCRIPT>X</SCRIPT>\xC0\xAF</HEAD>"
	got := asciiLower(in)
	if len(got) != len(in) {
		t.Fatalf("asciiLower changed byte length: in=%d out=%d", len(in), len(got))
	}
	if !strings.Contains(got, "<script>") || !strings.Contains(got, "</head>") {
		t.Errorf("asciiLower did not fold ASCII tag tokens: %q", got)
	}
	// Non-ASCII / invalid bytes are preserved verbatim.
	if got[0] != 0xB0 || got[1] != 0xA1 {
		t.Errorf("asciiLower mutated non-ASCII bytes")
	}
}

func tail(s string) string {
	const n = 120
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
