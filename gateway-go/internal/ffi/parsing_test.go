package ffi

import (
	"strings"
	"testing"
)

func TestExtractLinks_Empty(t *testing.T) {
	urls, err := ExtractLinks("", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("expected empty, got %v", urls)
	}
}

func TestExtractLinks_BareURLs(t *testing.T) {
	urls, err := ExtractLinks("Check https://example.com and https://rust-lang.org", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://example.com" {
		t.Errorf("expected https://example.com, got %s", urls[0])
	}
}

func TestExtractLinks_MaxLimit(t *testing.T) {
	urls, err := ExtractLinks("https://a.com https://b.com https://c.com", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(urls))
	}
}

func TestExtractLinks_SSRFBlocked(t *testing.T) {
	urls, err := ExtractLinks("https://example.com http://127.0.0.1/admin", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL (SSRF blocked), got %d: %v", len(urls), urls)
	}
}

func TestHtmlToMarkdown_Basic(t *testing.T) {
	text, title, err := HtmlToMarkdown("<html><head><title>Test</title></head><body><p>Hello world</p></body></html>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "world") {
		t.Errorf("expected text to contain Hello world, got: %s", text)
	}
	if title != "Test" {
		t.Errorf("expected title 'Test', got: %s", title)
	}
}

func TestHtmlToMarkdown_Empty(t *testing.T) {
	text, title, err := HtmlToMarkdown("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" || title != "" {
		t.Errorf("expected empty output, got text=%q title=%q", text, title)
	}
}

func TestBase64Estimate_Basic(t *testing.T) {
	est, err := Base64Estimate("AAAA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != 3 {
		t.Errorf("expected 3, got %d", est)
	}
}

func TestBase64Estimate_Padding(t *testing.T) {
	est, err := Base64Estimate("AA==")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != 1 {
		t.Errorf("expected 1, got %d", est)
	}
}

func TestBase64Estimate_Empty(t *testing.T) {
	est, err := Base64Estimate("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != 0 {
		t.Errorf("expected 0, got %d", est)
	}
}

func TestBase64Canonicalize_Valid(t *testing.T) {
	result, err := Base64Canonicalize(" A A A A ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "AAAA" {
		t.Errorf("expected AAAA, got %s", result)
	}
}

func TestBase64Canonicalize_Invalid(t *testing.T) {
	_, err := Base64Canonicalize("AAA")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseMediaTokens_NoMedia(t *testing.T) {
	text, urls, voice, err := ParseMediaTokens("Hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("expected original text, got: %s", text)
	}
	if len(urls) != 0 {
		t.Errorf("expected no URLs, got %v", urls)
	}
	if voice {
		t.Error("expected no audio_as_voice")
	}
}

func TestParseMediaTokens_WithURL(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("output\nMEDIA: https://example.com/img.png\ndone")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://example.com/img.png" {
		t.Errorf("expected one URL, got %v", urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: line should be stripped from text: %s", text)
	}
}

func TestParseMediaTokens_AudioAsVoice(t *testing.T) {
	_, _, voice, err := ParseMediaTokens("Hello [[audio_as_voice]]\nMEDIA: /tmp/voice.wav")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !voice {
		t.Error("expected audio_as_voice to be true")
	}
}

func TestParseMediaTokens_Empty(t *testing.T) {
	text, urls, voice, err := ParseMediaTokens("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" || len(urls) != 0 || voice {
		t.Errorf("expected empty output, got text=%q urls=%v voice=%v", text, urls, voice)
	}
}

func TestExtractLinks_Deduplication(t *testing.T) {
	urls, err := ExtractLinks("https://example.com and again https://example.com", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 {
		t.Errorf("expected 1 deduplicated URL, got %d: %v", len(urls), urls)
	}
}

func TestExtractLinks_DefaultMaxLinks(t *testing.T) {
	// maxLinks <= 0 defaults to 5
	input := "https://a.com https://b.com https://c.com https://d.com https://e.com https://f.com https://g.com"
	urls, err := ExtractLinks(input, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 5 {
		t.Errorf("expected 5 URLs (default max), got %d", len(urls))
	}
}

func TestExtractLinks_WhitespaceOnly(t *testing.T) {
	urls, err := ExtractLinks("   \n\t  ", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 0 {
		t.Errorf("expected empty for whitespace-only, got %v", urls)
	}
}

func TestHtmlToMarkdown_ScriptStripping(t *testing.T) {
	html := `<html><body><script>alert("xss")</script><p>Safe content</p><style>.bad{}</style></body></html>`
	text, _, err := HtmlToMarkdown(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(text, "alert") {
		t.Errorf("script content should be stripped, got: %s", text)
	}
	if strings.Contains(text, ".bad") {
		t.Errorf("style content should be stripped, got: %s", text)
	}
	if !strings.Contains(text, "Safe content") {
		t.Errorf("expected body content preserved, got: %s", text)
	}
}

func TestHtmlToMarkdown_EntityDecoding(t *testing.T) {
	html := `<p>A &amp; B &lt; C &gt; D &quot;E&quot; &#39;F&#39;</p>`
	text, _, err := HtmlToMarkdown(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(text, "A & B") {
		t.Errorf("expected &amp; decoded, got: %s", text)
	}
	if !strings.Contains(text, `"E"`) {
		t.Errorf("expected &quot; decoded, got: %s", text)
	}
}

func TestHtmlToMarkdown_NoTitle(t *testing.T) {
	text, title, err := HtmlToMarkdown("<body><p>Hello</p></body>")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if title != "" {
		t.Errorf("expected empty title, got %q", title)
	}
	if !strings.Contains(text, "Hello") {
		t.Errorf("expected Hello in text, got %q", text)
	}
}

func TestBase64Estimate_WithWhitespace(t *testing.T) {
	// "AAAA" with embedded whitespace should still estimate 3 bytes
	est, err := Base64Estimate("A A\nA\tA")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != 3 {
		t.Errorf("expected 3, got %d", est)
	}
}

func TestBase64Estimate_WhitespaceOnly(t *testing.T) {
	est, err := Base64Estimate("   \n\t  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if est != 0 {
		t.Errorf("expected 0 for whitespace-only, got %d", est)
	}
}

func TestBase64Canonicalize_Empty(t *testing.T) {
	_, err := Base64Canonicalize("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestBase64Canonicalize_WhitespaceOnly(t *testing.T) {
	_, err := Base64Canonicalize("   \t\n  ")
	if err == nil {
		t.Error("expected error for whitespace-only input")
	}
}

func TestBase64Canonicalize_WithNewlines(t *testing.T) {
	// Valid base64 with embedded newlines
	result, err := Base64Canonicalize("AQID\nBAUG")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "AQIDBAUG" {
		t.Errorf("expected AQIDBAUG, got %s", result)
	}
}

func TestParseMediaTokens_FilePath(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("Here is the image\nMEDIA: /tmp/photo.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "/tmp/photo.jpg" {
		t.Errorf("expected file path URL, got %v", urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: line should be stripped, got: %s", text)
	}
}

func TestParseMediaTokens_MultipleMedia(t *testing.T) {
	input := "text\nMEDIA: https://a.com/1.jpg\nmore text\nMEDIA: https://b.com/2.png"
	text, urls, _, err := ParseMediaTokens(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 2 {
		t.Errorf("expected 2 URLs, got %d: %v", len(urls), urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: lines should be stripped")
	}
}

func TestParseMediaTokens_InvalidMediaKept(t *testing.T) {
	// A MEDIA: line with no valid path should be kept in text
	text, urls, _, err := ParseMediaTokens("MEDIA: not a valid path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 0 {
		t.Errorf("expected no URLs for invalid media, got %v", urls)
	}
	if !strings.Contains(text, "MEDIA:") {
		t.Errorf("invalid MEDIA: line should be kept in text")
	}
}

func TestParseMediaTokens_FileScheme(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("MEDIA: file:///home/user/img.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 || urls[0] != "/home/user/img.png" {
		t.Errorf("expected stripped file:// path, got %v", urls)
	}
	_ = text
}

func TestParseMediaTokens_InsideFenceIgnored(t *testing.T) {
	input := "text\n```\nMEDIA: https://example.com/skip.png\n```\nMEDIA: https://example.com/keep.png"
	text, urls, _, err := ParseMediaTokens(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 URL (fence-skipped), got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://example.com/keep.png" {
		t.Errorf("expected keep.png, got %s", urls[0])
	}
	if !strings.Contains(text, "MEDIA: https://example.com/skip.png") {
		t.Errorf("fenced MEDIA line should be preserved in text")
	}
}

func TestParseMediaTokens_WindowsPath(t *testing.T) {
	_, urls, _, err := ParseMediaTokens("MEDIA: C:\\Users\\test\\photo.jpg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(urls) != 1 {
		t.Errorf("expected Windows path accepted, got %v", urls)
	}
}
