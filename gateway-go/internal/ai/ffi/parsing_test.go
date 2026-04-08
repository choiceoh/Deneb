package ffi

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestExtractLinks_Empty(t *testing.T) {
	urls := testutil.Must(ExtractLinks("", 5))
	if len(urls) != 0 {
		t.Fatalf("got %v, want empty", urls)
	}
}

func TestExtractLinks_BareURLs(t *testing.T) {
	urls := testutil.Must(ExtractLinks("Check https://example.com and https://rust-lang.org", 5))
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 URLs", len(urls), urls)
	}
	if urls[0] != "https://example.com" {
		t.Errorf("got %s, want https://example.com", urls[0])
	}
}

func TestExtractLinks_MaxLimit(t *testing.T) {
	urls := testutil.Must(ExtractLinks("https://a.com https://b.com https://c.com", 2))
	if len(urls) != 2 {
		t.Fatalf("got %d, want 2 URLs", len(urls))
	}
}

func TestExtractLinks_SSRFBlocked(t *testing.T) {
	urls := testutil.Must(ExtractLinks("https://example.com http://127.0.0.1/admin", 5))
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL (SSRF blocked)", len(urls), urls)
	}
}

func TestHTMLToMarkdown_Basic(t *testing.T) {
	text, title, err := HTMLToMarkdown("<html><head><title>Test</title></head><body><p>Hello world</p></body></html>")
	testutil.NoError(t, err)
	if !strings.Contains(text, "Hello") || !strings.Contains(text, "world") {
		t.Errorf("expected text to contain Hello world, got: %s", text)
	}
	if title != "Test" {
		t.Errorf("expected title 'Test', got: %s", title)
	}
}

func TestHTMLToMarkdown_Empty(t *testing.T) {
	text, title, err := HTMLToMarkdown("")
	testutil.NoError(t, err)
	if text != "" || title != "" {
		t.Errorf("got text=%q title=%q, want empty output", text, title)
	}
}

func TestBase64Estimate_Basic(t *testing.T) {
	est := testutil.Must(Base64Estimate("AAAA"))
	if est != 3 {
		t.Errorf("got %d, want 3", est)
	}
}

func TestBase64Estimate_Padding(t *testing.T) {
	est := testutil.Must(Base64Estimate("AA=="))
	if est != 1 {
		t.Errorf("got %d, want 1", est)
	}
}

func TestBase64Estimate_Empty(t *testing.T) {
	est := testutil.Must(Base64Estimate(""))
	if est != 0 {
		t.Errorf("got %d, want 0", est)
	}
}

func TestBase64Canonicalize_Valid(t *testing.T) {
	result := testutil.Must(Base64Canonicalize(" A A A A "))
	if result != "AAAA" {
		t.Errorf("got %s, want AAAA", result)
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
	testutil.NoError(t, err)
	if text != "Hello world" {
		t.Errorf("expected original text, got: %s", text)
	}
	if len(urls) != 0 {
		t.Errorf("got %v, want no URLs", urls)
	}
	if voice {
		t.Error("expected no audio_as_voice")
	}
}

func TestParseMediaTokens_WithURL(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("output\nMEDIA: https://example.com/img.png\ndone")
	testutil.NoError(t, err)
	if len(urls) != 1 || urls[0] != "https://example.com/img.png" {
		t.Errorf("got %v, want one URL", urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: line should be stripped from text: %s", text)
	}
}

func TestParseMediaTokens_AudioAsVoice(t *testing.T) {
	_, _, voice, err := ParseMediaTokens("Hello [[audio_as_voice]]\nMEDIA: /tmp/voice.wav")
	testutil.NoError(t, err)
	if !voice {
		t.Error("expected audio_as_voice to be true")
	}
}

func TestParseMediaTokens_Empty(t *testing.T) {
	text, urls, voice, err := ParseMediaTokens("")
	testutil.NoError(t, err)
	if text != "" || len(urls) != 0 || voice {
		t.Errorf("got text=%q urls=%v voice=%v, want empty output", text, urls, voice)
	}
}

func TestExtractLinks_Deduplication(t *testing.T) {
	urls := testutil.Must(ExtractLinks("https://example.com and again https://example.com", 5))
	if len(urls) != 1 {
		t.Errorf("got %d: %v, want 1 deduplicated URL", len(urls), urls)
	}
}

func TestExtractLinks_DefaultMaxLinks(t *testing.T) {
	// maxLinks <= 0 defaults to 5
	input := "https://a.com https://b.com https://c.com https://d.com https://e.com https://f.com https://g.com"
	urls := testutil.Must(ExtractLinks(input, 0))
	if len(urls) != 5 {
		t.Errorf("got %d, want 5 URLs (default max)", len(urls))
	}
}

func TestExtractLinks_WhitespaceOnly(t *testing.T) {
	urls := testutil.Must(ExtractLinks("   \n\t  ", 5))
	if len(urls) != 0 {
		t.Errorf("got %v, want empty for whitespace-only", urls)
	}
}

func TestHTMLToMarkdown_ScriptStripping(t *testing.T) {
	html := `<html><body><script>alert("xss")</script><p>Safe content</p><style>.bad{}</style></body></html>`
	text, _, err := HTMLToMarkdown(html)
	testutil.NoError(t, err)
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

func TestHTMLToMarkdown_EntityDecoding(t *testing.T) {
	html := `<p>A &amp; B &lt; C &gt; D &quot;E&quot; &#39;F&#39;</p>`
	text, _, err := HTMLToMarkdown(html)
	testutil.NoError(t, err)
	if !strings.Contains(text, "A & B") {
		t.Errorf("expected &amp; decoded, got: %s", text)
	}
	if !strings.Contains(text, `"E"`) {
		t.Errorf("expected &quot; decoded, got: %s", text)
	}
}

func TestHTMLToMarkdown_NoTitle(t *testing.T) {
	text, title, err := HTMLToMarkdown("<body><p>Hello</p></body>")
	testutil.NoError(t, err)
	if title != "" {
		t.Errorf("got %q, want empty title", title)
	}
	if !strings.Contains(text, "Hello") {
		t.Errorf("got %q, want Hello in text", text)
	}
}

func TestBase64Estimate_WithWhitespace(t *testing.T) {
	// "AAAA" with embedded whitespace should still estimate 3 bytes
	est := testutil.Must(Base64Estimate("A A\nA\tA"))
	if est != 3 {
		t.Errorf("got %d, want 3", est)
	}
}

func TestBase64Estimate_WhitespaceOnly(t *testing.T) {
	est := testutil.Must(Base64Estimate("   \n\t  "))
	if est != 0 {
		t.Errorf("got %d, want 0 for whitespace-only", est)
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
	result := testutil.Must(Base64Canonicalize("AQID\nBAUG"))
	if result != "AQIDBAUG" {
		t.Errorf("got %s, want AQIDBAUG", result)
	}
}

func TestParseMediaTokens_FilePath(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("Here is the image\nMEDIA: /tmp/photo.jpg")
	testutil.NoError(t, err)
	if len(urls) != 1 || urls[0] != "/tmp/photo.jpg" {
		t.Errorf("got %v, want file path URL", urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: line should be stripped, got: %s", text)
	}
}

func TestParseMediaTokens_MultipleMedia(t *testing.T) {
	input := "text\nMEDIA: https://a.com/1.jpg\nmore text\nMEDIA: https://b.com/2.png"
	text, urls, _, err := ParseMediaTokens(input)
	testutil.NoError(t, err)
	if len(urls) != 2 {
		t.Errorf("got %d: %v, want 2 URLs", len(urls), urls)
	}
	if strings.Contains(text, "MEDIA:") {
		t.Errorf("MEDIA: lines should be stripped")
	}
}

func TestParseMediaTokens_InvalidMediaKept(t *testing.T) {
	// A MEDIA: line with no valid path should be kept in text
	text, urls, _, err := ParseMediaTokens("MEDIA: not a valid path")
	testutil.NoError(t, err)
	if len(urls) != 0 {
		t.Errorf("got %v, want no URLs for invalid media", urls)
	}
	if !strings.Contains(text, "MEDIA:") {
		t.Errorf("invalid MEDIA: line should be kept in text")
	}
}

func TestParseMediaTokens_FileScheme(t *testing.T) {
	text, urls, _, err := ParseMediaTokens("MEDIA: file:///home/user/img.png")
	testutil.NoError(t, err)
	if len(urls) != 1 || urls[0] != "/home/user/img.png" {
		t.Errorf("got %v, want stripped file:// path", urls)
	}
	_ = text
}

func TestParseMediaTokens_InsideFenceIgnored(t *testing.T) {
	input := "text\n```\nMEDIA: https://example.com/skip.png\n```\nMEDIA: https://example.com/keep.png"
	text, urls, _, err := ParseMediaTokens(input)
	testutil.NoError(t, err)
	if len(urls) != 1 {
		t.Fatalf("got %d: %v, want 1 URL (fence-skipped)", len(urls), urls)
	}
	if urls[0] != "https://example.com/keep.png" {
		t.Errorf("got %s, want keep.png", urls[0])
	}
	if !strings.Contains(text, "MEDIA: https://example.com/skip.png") {
		t.Errorf("fenced MEDIA line should be preserved in text")
	}
}

func TestParseMediaTokens_WindowsPath(t *testing.T) {
	_, urls, _, err := ParseMediaTokens("MEDIA: C:\\Users\\test\\photo.jpg")
	testutil.NoError(t, err)
	if len(urls) != 1 {
		t.Errorf("got %v, want Windows path accepted", urls)
	}
}
