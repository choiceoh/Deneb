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
