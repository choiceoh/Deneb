package autoreply

import "testing"

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
		t.Fatalf("expected empty text for silent reply, got %q", result.Text)
	}
}

func TestParseReplyDirectives_ReplyToCurrent(t *testing.T) {
	result := ParseReplyDirectives("Hello [[reply_to_current]]", "msg-123", "")
	if !result.ReplyToCurrent {
		t.Fatal("expected reply-to-current")
	}
	if result.ReplyToID != "msg-123" {
		t.Fatalf("expected replyToID=msg-123, got %q", result.ReplyToID)
	}
	if result.Text != "Hello" {
		t.Fatalf("expected text without tag, got %q", result.Text)
	}
}

func TestParseReplyDirectives_ReplyToSpecific(t *testing.T) {
	result := ParseReplyDirectives("Response [[reply_to:abc-456]]", "", "")
	if result.ReplyToID != "abc-456" {
		t.Fatalf("expected replyToID=abc-456, got %q", result.ReplyToID)
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

func TestSplitMediaFromOutput(t *testing.T) {
	text, urls, url, _ := splitMediaFromOutput("Hello\nhttps://example.com/image.png\nWorld")
	if url != "https://example.com/image.png" {
		t.Fatalf("expected media URL, got %q", url)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 media URL, got %d", len(urls))
	}
	if text != "Hello\nWorld" {
		t.Fatalf("unexpected remaining text: %q", text)
	}
}

func TestSplitMediaFromOutput_NoMedia(t *testing.T) {
	text, urls, url, _ := splitMediaFromOutput("Just plain text")
	if url != "" {
		t.Fatalf("expected no media URL, got %q", url)
	}
	if len(urls) != 0 {
		t.Fatalf("expected 0 media URLs, got %d", len(urls))
	}
	if text != "Just plain text" {
		t.Fatalf("unexpected text: %q", text)
	}
}
