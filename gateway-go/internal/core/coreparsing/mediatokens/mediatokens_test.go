package mediatokens

import (
	"strings"
	"testing"
)



func TestParse_SingleURL(t *testing.T) {
	r := Parse("Here is an image\nMEDIA: https://example.com/image.png")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "https://example.com/image.png" {
		t.Errorf("got %v, want one URL", r.MediaURLs)
	}
	if strings.Contains(r.Text, "MEDIA:") {
		t.Errorf("MEDIA: line should be stripped: %s", r.Text)
	}
	if !strings.Contains(r.Text, "Here is an image") {
		t.Errorf("got %q, want text preserved", r.Text)
	}
}

func TestParse_LocalPath(t *testing.T) {
	r := Parse("MEDIA: /tmp/output.wav\nDone.")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "/tmp/output.wav" {
		t.Errorf("got %v, want /tmp/output.wav", r.MediaURLs)
	}
	if !strings.Contains(r.Text, "Done.") {
		t.Errorf("got %q, want Done. in text", r.Text)
	}
}

func TestParse_FileProtocolNormalized(t *testing.T) {
	r := Parse("MEDIA: file:///tmp/audio.mp3")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "/tmp/audio.mp3" {
		t.Errorf("got %v, want stripped file:// path", r.MediaURLs)
	}
}

func TestParse_InsideFenceIgnored(t *testing.T) {
	input := "text\n```\nMEDIA: https://example.com/skip.png\n```\nMEDIA: https://example.com/keep.png"
	r := Parse(input)
	if len(r.MediaURLs) != 1 {
		t.Fatalf("got %d: %v, want 1 URL (fence-skipped)", len(r.MediaURLs), r.MediaURLs)
	}
	if r.MediaURLs[0] != "https://example.com/keep.png" {
		t.Errorf("got %s, want keep.png", r.MediaURLs[0])
	}
	if !strings.Contains(r.Text, "MEDIA: https://example.com/skip.png") {
		t.Errorf("fenced MEDIA line should be preserved in text")
	}
}

func TestParse_AudioAsVoice(t *testing.T) {
	r := Parse("Hello [[audio_as_voice]]\nMEDIA: /tmp/voice.wav")
	if !r.AudioAsVoice {
		t.Error("expected audio_as_voice to be true")
	}
	if strings.Contains(r.Text, "[[audio_as_voice]]") {
		t.Errorf("tag should be stripped from text: %s", r.Text)
	}
}


func TestParse_MultipleMedia(t *testing.T) {
	input := "MEDIA: https://a.com/1.png\ntext\nMEDIA: https://b.com/2.png"
	r := Parse(input)
	if len(r.MediaURLs) != 2 {
		t.Errorf("got %d: %v, want 2 URLs", len(r.MediaURLs), r.MediaURLs)
	}
}

func TestParse_BacktickWrapped(t *testing.T) {
	r := Parse("MEDIA: `https://example.com/img.png`")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "https://example.com/img.png" {
		t.Errorf("got %v, want backtick-unwrapped URL", r.MediaURLs)
	}
}

func TestParse_QuotedPathWithSpaces(t *testing.T) {
	r := Parse(`MEDIA: "/tmp/my file with spaces.mp3"`)
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "/tmp/my file with spaces.mp3" {
		t.Errorf("got %v, want quoted path", r.MediaURLs)
	}
}





func TestParse_DirectiveKeyValue(t *testing.T) {
	r := Parse("Hello [[audio_as_voice]] [[format=wav]]\nMEDIA: /tmp/voice.wav")
	if !r.AudioAsVoice {
		t.Error("expected audio_as_voice")
	}
	if strings.Contains(r.Text, "[[") {
		t.Errorf("directives should be stripped: %s", r.Text)
	}
}





// --- Rust parity: unclosed fence extends to end ---

func TestParse_UnclosedFence(t *testing.T) {
	input := "text\n```\nMEDIA: https://example.com/skip.png"
	r := Parse(input)
	// Unclosed fence — MEDIA line inside should be kept as text.
	if len(r.MediaURLs) != 0 {
		t.Errorf("got %v, want no URLs (inside unclosed fence)", r.MediaURLs)
	}
	if !strings.Contains(r.Text, "MEDIA: https://example.com/skip.png") {
		t.Errorf("fenced MEDIA line should be preserved: %s", r.Text)
	}
}

// --- Rust parity: tilde fence ---

func TestParse_TildeFence(t *testing.T) {
	input := "text\n~~~\nMEDIA: https://example.com/skip.png\n~~~\nMEDIA: https://example.com/keep.png"
	r := Parse(input)
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "https://example.com/keep.png" {
		t.Errorf("got %v, want only keep.png", r.MediaURLs)
	}
}

// --- Rust parity: empty payload kept ---


// --- Rust parity: whitespace-only input ---


// --- Rust parity: MEDIA: with leading whitespace ---


// --- Rust parity: likely local path drops line ---


// --- Rust parity: collapse_whitespace ---




// --- Rust parity: isBareFilename edge cases ---




