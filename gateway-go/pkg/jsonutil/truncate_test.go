package jsonutil

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTruncateStringLeaves_ShortString(t *testing.T) {
	in := `{"name":"hi"}`
	got := TruncateStringLeaves(in, 100)
	if got != in {
		t.Errorf("short string should pass through: got %q, want %q", got, in)
	}
}

func TestTruncateStringLeaves_LongString(t *testing.T) {
	long := strings.Repeat("a", 500)
	in := `{"content":"` + long + `"}`
	got := TruncateStringLeaves(in, 50)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("truncated output is not valid JSON: %v (out=%q)", err, got)
	}
	text, ok := parsed["content"].(string)
	if !ok {
		t.Fatalf("content is not a string in truncated output: %T", parsed["content"])
	}
	if !strings.HasSuffix(text, "...[truncated]") {
		t.Errorf("long string missing truncation marker: %q", text)
	}
	if len([]rune(text)) != 50+len([]rune("...[truncated]")) {
		t.Errorf("unexpected rune length: %d", len([]rune(text)))
	}
}

func TestTruncateStringLeaves_NestedStructure(t *testing.T) {
	long := strings.Repeat("x", 300)
	in := `{"outer":{"inner":["short","` + long + `"]},"n":42,"b":true}`
	got := TruncateStringLeaves(in, 20)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("invalid JSON after truncation: %v", err)
	}
	outer := parsed["outer"].(map[string]any)
	inner := outer["inner"].([]any)
	if inner[0].(string) != "short" {
		t.Errorf("short leaf should be intact, got %q", inner[0])
	}
	if !strings.HasSuffix(inner[1].(string), "...[truncated]") {
		t.Errorf("long nested leaf should be truncated, got %q", inner[1])
	}
	// Non-string scalars must be preserved exactly.
	if n, ok := parsed["n"].(float64); !ok || n != 42 {
		t.Errorf("number preserved incorrectly: %v", parsed["n"])
	}
	if b, ok := parsed["b"].(bool); !ok || !b {
		t.Errorf("bool preserved incorrectly: %v", parsed["b"])
	}
}

func TestTruncateStringLeaves_InvalidJSON(t *testing.T) {
	in := `{"broken":`
	got := TruncateStringLeaves(in, 100)
	if got != in {
		t.Errorf("invalid JSON should pass through unchanged: got %q", got)
	}
}

func TestTruncateStringLeaves_MultiByteBoundary(t *testing.T) {
	// Korean text: each character is 3 bytes but 1 rune. A byte-count
	// truncator would cut mid-rune; we must stay rune-safe.
	in := `{"text":"안녕하세요 반가워요 잘 부탁드립니다 정말입니다 계속됩니다"}`
	got := TruncateStringLeaves(in, 5)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("multibyte input produced invalid JSON: %v (out=%q)", err, got)
	}
	text := parsed["text"].(string)
	if !strings.HasSuffix(text, "...[truncated]") {
		t.Errorf("expected truncation marker in %q", text)
	}
	if !strings.HasPrefix(text, "안녕하세요") {
		t.Errorf("expected first 5 Korean runes preserved, got prefix of %q", text)
	}
}
