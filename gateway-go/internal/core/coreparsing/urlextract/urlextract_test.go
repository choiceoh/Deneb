package urlextract

import (
	"testing"
)

func TestExtractLinks_BareURLs(t *testing.T) {
	urls := ExtractLinks("Check https://example.com and https://rust-lang.org", 5)
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 URLs", len(urls), urls)
	}
	if urls[0] != "https://example.com" {
		t.Errorf("got %s, want https://example.com", urls[0])
	}
	if urls[1] != "https://rust-lang.org" {
		t.Errorf("got %s, want https://rust-lang.org", urls[1])
	}
}

func TestExtractLinks_MarkdownLinksStripped(t *testing.T) {
	text := "See [Docs](https://docs.example.com) and https://bare.example.com"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL (markdown stripped)", len(urls), urls)
	}
	if urls[0] != "https://bare.example.com" {
		t.Errorf("got %s, want https://bare.example.com", urls[0])
	}
}

func TestExtractLinks_Deduplication(t *testing.T) {
	urls := ExtractLinks("https://example.com https://example.com https://example.com", 5)
	if len(urls) != 1 {
		t.Errorf("got %d: %v, want 1 deduplicated URL", len(urls), urls)
	}
}

func TestExtractLinks_MaxLimit(t *testing.T) {
	urls := ExtractLinks("https://a.com https://b.com https://c.com https://d.com", 2)
	if len(urls) != 2 {
		t.Fatalf("got %d, want 2 URLs", len(urls))
	}
}
func TestExtractLinks_SSRFBlocked(t *testing.T) {
	text := "https://example.com http://127.0.0.1/admin http://169.254.169.254/metadata"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL (SSRF blocked)", len(urls), urls)
	}
	if urls[0] != "https://example.com" {
		t.Errorf("got %s, want https://example.com", urls[0])
	}
}
func TestStripURLTail_TrailingPunctuation(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com,", "https://example.com"},
		{"https://example.com.", "https://example.com"},
		{"https://example.com;", "https://example.com"},
		{"https://example.com!", "https://example.com"},
		{`https://example.com"),`, "https://example.com"},
	}
	for _, tt := range tests {
		got := stripURLTail(tt.input)
		if got != tt.want {
			t.Errorf("stripURLTail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripURLTail_BalancedParens(t *testing.T) {
	// Wikipedia-style URL with balanced parens should be kept.
	url := "https://en.wikipedia.org/wiki/Rust_(programming_language)"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced parens preserved", got)
	}
}

func TestStripURLTail_UnbalancedClosingParen(t *testing.T) {
	urls := findBareURLs("(see https://example.com)")
	if len(urls) != 1 || urls[0] != "https://example.com" {
		t.Errorf("got %v, want unbalanced paren stripped", urls)
	}
}
func TestExtractLinks_JSONArray(t *testing.T) {
	input := `["https://github.com/choiceoh/deneb/releases/latest/download/latest.json"],`
	urls := ExtractLinks(input, 5)
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL", len(urls), urls)
	}
	if urls[0] != "https://github.com/choiceoh/deneb/releases/latest/download/latest.json" {
		t.Errorf("unexpected URL: %s", urls[0])
	}
}

func TestExtractLinks_MultibyteMDLinks(t *testing.T) {
	// Korean text with markdown link must not corrupt multibyte chars.
	text := "한국어 [링크](https://docs.example.com) 텍스트 https://bare.example.com 끝"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL", len(urls), urls)
	}
	if urls[0] != "https://bare.example.com" {
		t.Errorf("got %s, want https://bare.example.com", urls[0])
	}

	// Verify Korean chars preserved.
	stripped := stripMarkdownLinks(text)
	for _, kw := range []string{"한국어", "텍스트", "끝"} {
		if !contains(stripped, kw) {
			t.Errorf("expected %q preserved in stripped text: %s", kw, stripped)
		}
	}
}

// --- Rust parity: trailing colon stripped ---
// --- Rust parity: balanced brackets for all 4 bracket types ---

func TestStripURLTail_BalancedSquareBrackets(t *testing.T) {
	// Balanced [] should be preserved.
	url := "https://example.com/path[0]"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced [] preserved", got)
	}
}
func TestStripURLTail_BalancedCurlyBraces(t *testing.T) {
	url := "https://example.com/path{id}"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced {} preserved", got)
	}
}

// --- Rust parity: markdown link with non-HTTP URL (should not be stripped) ---
// --- Rust parity: minimum URL length (>7 chars) ---
// --- Rust parity: case-insensitive scheme detection ---
// --- Rust parity: multiple markdown links in same text ---
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
