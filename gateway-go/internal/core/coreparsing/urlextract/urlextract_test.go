package urlextract

import (
	"testing"
)

func TestExtractLinks_Empty(t *testing.T) {
	if urls := ExtractLinks("", 5); len(urls) != 0 {
		t.Fatalf("got %v, want empty", urls)
	}
	if urls := ExtractLinks("   ", 5); len(urls) != 0 {
		t.Fatalf("got %v, want empty for whitespace", urls)
	}
}

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

func TestExtractLinks_DefaultMaxLinks(t *testing.T) {
	input := "https://a.com https://b.com https://c.com https://d.com https://e.com https://f.com https://g.com"
	urls := ExtractLinks(input, 0)
	if len(urls) != 5 {
		t.Errorf("got %d, want 5 URLs (default max)", len(urls))
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

func TestExtractLinks_FTPScheme(t *testing.T) {
	text := "ftp://files.example.com ssh://server.example.com https://ok.example.com"
	urls := ExtractLinks(text, 5)
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 URLs", len(urls), urls)
	}
	if urls[0] != "ftp://files.example.com" {
		t.Errorf("got %s, want ftp://files.example.com", urls[0])
	}
	if urls[1] != "https://ok.example.com" {
		t.Errorf("got %s, want https://ok.example.com", urls[1])
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

func TestStripURLTail_TrailingQuotes(t *testing.T) {
	urls := findBareURLs(`"https://example.com"`)
	if len(urls) != 1 || urls[0] != "https://example.com" {
		t.Errorf("got %v, want quotes stripped", urls)
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

func TestExtractLinks_Emoji(t *testing.T) {
	text := "🌍 [link](https://skip.com) 🚀 https://keep.com 🎉"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 || urls[0] != "https://keep.com" {
		t.Errorf("got %v, want https://keep.com", urls)
	}
}

func TestExtractLinks_NestedBrackets(t *testing.T) {
	text := "[link [sub] text](https://skip.com) https://keep.com"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 || urls[0] != "https://keep.com" {
		t.Errorf("got %v, want only bare URL", urls)
	}
}

// --- Rust parity: trailing colon stripped ---

func TestStripURLTail_TrailingColon(t *testing.T) {
	got := stripURLTail("https://example.com:")
	if got != "https://example.com" {
		t.Errorf("got %s, want colon stripped", got)
	}
}

// --- Rust parity: balanced brackets for all 4 bracket types ---

func TestStripURLTail_BalancedSquareBrackets(t *testing.T) {
	// Balanced [] should be preserved.
	url := "https://example.com/path[0]"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced [] preserved", got)
	}
}

func TestStripURLTail_UnbalancedSquareBracket(t *testing.T) {
	urls := findBareURLs("[see https://example.com]")
	if len(urls) != 1 || urls[0] != "https://example.com" {
		t.Errorf("got %v, want unbalanced ] stripped", urls)
	}
}

func TestStripURLTail_BalancedCurlyBraces(t *testing.T) {
	url := "https://example.com/path{id}"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced {} preserved", got)
	}
}

func TestStripURLTail_BalancedAngleBrackets(t *testing.T) {
	url := "https://example.com/path<key>"
	got := stripURLTail(url)
	if got != url {
		t.Errorf("got %s, want balanced <> preserved", got)
	}
}

// --- Rust parity: markdown link with non-HTTP URL (should not be stripped) ---

func TestStripMarkdownLinks_NonHTTPNotStripped(t *testing.T) {
	text := "[label](ftp://files.example.com) https://keep.com"
	urls := ExtractLinks(text, 5)
	// ftp link is inside markdown syntax but matchMarkdownLink only matches http/https,
	// so ftp:// should be found as bare URL + keep.com.
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 URLs", len(urls), urls)
	}
}

// --- Rust parity: minimum URL length (>7 chars) ---

func TestFindBareURLs_MinLength(t *testing.T) {
	// "ftp://x" is exactly 7 chars — should be rejected (>7 required).
	urls := findBareURLs("ftp://x")
	if len(urls) != 0 {
		t.Errorf("got %v, want ftp://x rejected (too short)", urls)
	}
	// "ftp://xy" is 8 chars — should be accepted.
	urls = findBareURLs("ftp://xy")
	if len(urls) != 1 {
		t.Errorf("got %v, want ftp://xy accepted", urls)
	}
}

// --- Rust parity: case-insensitive scheme detection ---

func TestExtractLinks_CaseInsensitiveScheme(t *testing.T) {
	urls := ExtractLinks("HTTPS://EXAMPLE.COM and Http://Other.Com/path", 5)
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 URLs", len(urls), urls)
	}
}

// --- Rust parity: multiple markdown links in same text ---

func TestExtractLinks_MultipleMarkdownLinks(t *testing.T) {
	text := "[a](https://skip1.com) [b](https://skip2.com) https://keep.com"
	urls := ExtractLinks(text, 5)
	if len(urls) != 1 || urls[0] != "https://keep.com" {
		t.Errorf("got %v, want only bare URL", urls)
	}
}

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
