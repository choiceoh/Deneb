package llm

import (
	"encoding/json"
	"testing"
	"unicode/utf8"
)

// jsonDecode marshals s with the standard library (the behavior we want to match
// for invalid UTF-8) and decodes it back, so the comparison is on the decoded
// string and not on cosmetic escape differences (e.g. \b vs ).
func jsonDecode(t *testing.T, encoded []byte) string {
	t.Helper()
	var got string
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("not valid JSON: %v (%q)", err, encoded)
	}
	return got
}

// appendJSONString must emit valid UTF-8 even for invalid input — invalid bytes
// become U+FFFD, like encoding/json — so the request body is never invalid UTF-8
// on the wire (a strict provider would otherwise 400 or mangle it).
func TestAppendJSONString_InvalidUTF8(t *testing.T) {
	inputs := []string{
		"plain ascii",
		"한국어 valid",
		"emoji 😀 4-byte",
		"tab\tnl\ncr\rbell\bff\f",
		"quote \" backslash \\ slash /",
		"a\x80b",        // lone continuation byte
		"\xff\xfe",      // invalid bytes
		"trunc\xec\x95", // truncated 3-byte sequence
		"\xed\xa0\x80",  // UTF-8-encoded surrogate (invalid per RFC 8259)
		"real � kept",   // an actual U+FFFD must survive unchanged
	}
	for _, in := range inputs {
		out := appendJSONString(nil, in)

		// 1. Output must always be valid UTF-8 (the bug: raw invalid bytes).
		if !utf8.Valid(out) {
			t.Errorf("appendJSONString(%q) emitted invalid UTF-8: %x", in, out)
		}
		// 2. Output must be valid JSON and decode to the same string the stdlib
		//    produces (U+FFFD where the input was invalid; verbatim otherwise).
		got := jsonDecode(t, out)
		want := jsonDecode(t, mustMarshal(t, in))
		if got != want {
			t.Errorf("appendJSONString(%q) decodes to %q, want %q (stdlib)", in, got, want)
		}
	}
}

func mustMarshal(t *testing.T, s string) []byte {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("json.Marshal(%q): %v", s, err)
	}
	return b
}
