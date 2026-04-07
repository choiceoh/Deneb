package chatport

import "testing"

func TestReplyDirectives_FieldAccess(t *testing.T) {
	rd := ReplyDirectives{
		Text:           "hello",
		MediaURLs:      []string{"https://example.com/a.png", "https://example.com/b.png"},
		MediaURL:       "https://example.com/a.png",
		ReplyToID:      "msg_123",
		ReplyToCurrent: true,
		ReplyToTag:     true,
		AudioAsVoice:   true,
		IsSilent:       true,
	}

	if rd.Text != "hello" {
		t.Errorf("Text = %q, want %q", rd.Text, "hello")
	}
	if len(rd.MediaURLs) != 2 {
		t.Errorf("MediaURLs len = %d, want 2", len(rd.MediaURLs))
	}
	if rd.MediaURLs[0] != "https://example.com/a.png" {
		t.Errorf("MediaURLs[0] = %q, want %q", rd.MediaURLs[0], "https://example.com/a.png")
	}
	if rd.MediaURL != "https://example.com/a.png" {
		t.Errorf("MediaURL = %q, want %q", rd.MediaURL, "https://example.com/a.png")
	}
	if rd.ReplyToID != "msg_123" {
		t.Errorf("ReplyToID = %q, want %q", rd.ReplyToID, "msg_123")
	}
	if !rd.ReplyToCurrent {
		t.Error("ReplyToCurrent = false, want true")
	}
	if !rd.ReplyToTag {
		t.Error("ReplyToTag = false, want true")
	}
	if !rd.AudioAsVoice {
		t.Error("AudioAsVoice = false, want true")
	}
	if !rd.IsSilent {
		t.Error("IsSilent = false, want true")
	}
}

func TestReplyDirectives_ZeroValue(t *testing.T) {
	var rd ReplyDirectives

	if rd.Text != "" {
		t.Errorf("zero Text = %q, want empty", rd.Text)
	}
	if rd.MediaURLs != nil {
		t.Errorf("zero MediaURLs = %v, want nil", rd.MediaURLs)
	}
	if rd.MediaURL != "" {
		t.Errorf("zero MediaURL = %q, want empty", rd.MediaURL)
	}
	if rd.ReplyToID != "" {
		t.Errorf("zero ReplyToID = %q, want empty", rd.ReplyToID)
	}
	if rd.ReplyToCurrent {
		t.Error("zero ReplyToCurrent = true, want false")
	}
	if rd.ReplyToTag {
		t.Error("zero ReplyToTag = true, want false")
	}
	if rd.AudioAsVoice {
		t.Error("zero AudioAsVoice = true, want false")
	}
	if rd.IsSilent {
		t.Error("zero IsSilent = true, want false")
	}
}
