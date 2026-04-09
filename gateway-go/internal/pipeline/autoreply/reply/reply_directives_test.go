package reply

import (
	"strings"
	"testing"
)

func TestParseReplyDirectives_Basic(t *testing.T) {
	result := ParseReplyDirectives("Hello world", "", "")
	if result.Text != "Hello world" {
		t.Fatalf("unexpected text: %q", result.Text)
	}
	if result.IsSilent {
		t.Fatal("expected not silent")
	}
	if result.ReplyToCurrent {
		t.Fatal("expected not reply-to-current")
	}
}

func TestParseReplyDirectives_Silent(t *testing.T) {
	result := ParseReplyDirectives("NO_REPLY", "", "")
	if !result.IsSilent {
		t.Fatal("expected silent")
	}
	if result.Text != "" {
		t.Fatalf("got %q, want empty text for silent reply", result.Text)
	}
}

func TestParseReplyDirectives_ReplyToCurrent(t *testing.T) {
	result := ParseReplyDirectives("Hello [[reply_to_current]]", "msg-123", "")
	if !result.ReplyToCurrent {
		t.Fatal("expected reply-to-current")
	}
	if result.ReplyToID != "msg-123" {
		t.Fatalf("got %q, want replyToID=msg-123", result.ReplyToID)
	}
	if result.Text != "Hello" {
		t.Fatalf("got %q, want text without tag", result.Text)
	}
}

func TestParseReplyDirectives_ReplyToSpecific(t *testing.T) {
	result := ParseReplyDirectives("Response [[reply_to:abc-456]]", "", "")
	if result.ReplyToID != "abc-456" {
		t.Fatalf("got %q, want replyToID=abc-456", result.ReplyToID)
	}
	if result.ReplyToCurrent {
		t.Fatal("expected not reply-to-current")
	}
	if !result.ReplyToTag {
		t.Fatal("expected ReplyToTag")
	}
}

func TestParseReplyDirectives_AudioAsVoice(t *testing.T) {
	result := ParseReplyDirectives("[[audio_as_voice]] hello", "", "")
	if !result.AudioAsVoice {
		t.Fatal("expected audioAsVoice")
	}
}

func TestParseReplyDirectives_MediaToken(t *testing.T) {
	result := ParseReplyDirectives("Here's the image:\nMEDIA: https://example.com/photo.jpg\nDone!", "", "")
	if result.MediaURL != "https://example.com/photo.jpg" {
		t.Fatalf("got %q, want media URL", result.MediaURL)
	}
	if len(result.MediaURLs) != 1 {
		t.Fatalf("got %d, want 1 media URL", len(result.MediaURLs))
	}
	// The MEDIA: line should be stripped from text.
	if strings.Contains(result.Text, "MEDIA:") {
		t.Fatalf("MEDIA: token should be stripped from text: %q", result.Text)
	}
}

func TestParseReplyDirectives_StripsLeakedToolCall(t *testing.T) {
	raw := "<function=read>\n<arg_key>file_path</arg_key>\n<arg_value>/tmp/test.go</arg_value>\n</tool_call>\n작업을 완료했습니다."
	result := ParseReplyDirectives(raw, "", "")
	if strings.Contains(result.Text, "<function=") {
		t.Fatalf("leaked tool-call markup should be stripped, got %q", result.Text)
	}
	if result.Text != "작업을 완료했습니다." {
		t.Fatalf("got %q, want cleaned text", result.Text)
	}
}

// --- splitMediaFromOutput tests ---

func TestSplitMediaFromOutput_MediaToken(t *testing.T) {
	text, urls, url, _ := splitMediaFromOutput("Hello\nMEDIA: https://example.com/image.png\nWorld")
	if url != "https://example.com/image.png" {
		t.Fatalf("got %q, want media URL", url)
	}
	if len(urls) != 1 {
		t.Fatalf("got %d, want 1 media URL", len(urls))
	}
	if strings.Contains(text, "MEDIA:") {
		t.Fatalf("MEDIA: should be stripped from text: %q", text)
	}
}

func TestSplitMediaFromOutput_NoMedia(t *testing.T) {
	text, urls, url, _ := splitMediaFromOutput("Just plain text")
	if url != "" {
		t.Fatalf("got %q, want no media URL", url)
	}
	if len(urls) != 0 {
		t.Fatalf("got %d, want 0 media URLs", len(urls))
	}
	if text != "Just plain text" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestSplitMediaFromOutput_LocalPath(t *testing.T) {
	text, urls, url, _ := splitMediaFromOutput("MEDIA: /tmp/image.png")
	if url != "/tmp/image.png" {
		t.Fatalf("got %q, want local path", url)
	}
	if len(urls) != 1 {
		t.Fatalf("got %d, want 1 url", len(urls))
	}
	if text != "" {
		t.Fatalf("got %q, want empty text after MEDIA: extraction", text)
	}
}

func TestSplitMediaFromOutput_FileURL(t *testing.T) {
	text, urls, _, _ := splitMediaFromOutput("MEDIA: file:///home/user/photo.jpg")
	if len(urls) != 1 || urls[0] != "/home/user/photo.jpg" {
		t.Fatalf("got %v, want file:// stripped path", urls)
	}
	if text != "" {
		t.Fatalf("got %q, want empty text", text)
	}
}

func TestSplitMediaFromOutput_InsideFence(t *testing.T) {
	input := "Hello\n```\nMEDIA: https://example.com/fake.png\n```\nWorld"
	text, urls, _, _ := splitMediaFromOutput(input)
	// MEDIA: inside a code fence should NOT be extracted.
	if len(urls) != 0 {
		t.Fatalf("got %d: %v, want 0 media URLs (inside fence)", len(urls), urls)
	}
	if !strings.Contains(text, "MEDIA:") {
		t.Fatalf("MEDIA: inside fence should be preserved in text: %q", text)
	}
}

func TestSplitMediaFromOutput_MultipleMedia(t *testing.T) {
	input := "MEDIA: https://a.com/1.png\nSome text\nMEDIA: https://b.com/2.jpg"
	text, urls, url, _ := splitMediaFromOutput(input)
	if len(urls) != 2 {
		t.Fatalf("got %d: %v, want 2 media URLs", len(urls), urls)
	}
	if url != "https://a.com/1.png" {
		t.Fatalf("got %q, want first URL", url)
	}
	if text != "Some text" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestSplitMediaFromOutput_AudioTag(t *testing.T) {
	text, _, _, audioAsVoice := splitMediaFromOutput("Hello [[audio_as_voice]] world")
	if !audioAsVoice {
		t.Fatal("expected audioAsVoice")
	}
	if strings.Contains(text, "[[audio_as_voice]]") {
		t.Fatalf("audio tag should be stripped: %q", text)
	}
}

func TestSplitMediaFromOutput_VoiceTag(t *testing.T) {
	_, _, _, audioAsVoice := splitMediaFromOutput("Hello [[voice]] world")
	if !audioAsVoice {
		t.Fatal("expected audioAsVoice from [[voice]] tag")
	}
}

func TestSplitMediaFromOutput_Empty(t *testing.T) {
	text, urls, _, _ := splitMediaFromOutput("")
	if text != "" || len(urls) != 0 {
		t.Fatalf("expected empty result for empty input")
	}
}

func TestSplitMediaFromOutput_RelativePath(t *testing.T) {
	_, urls, _, _ := splitMediaFromOutput("MEDIA: ./output/image.png")
	if len(urls) != 1 {
		t.Fatalf("got %d, want 1 URL for relative path", len(urls))
	}
}

