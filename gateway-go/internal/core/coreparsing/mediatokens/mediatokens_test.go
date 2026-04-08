package mediatokens

import (
	"strings"
	"testing"
)

func TestParse_Empty(t *testing.T) {
	r := Parse("")
	if r.Text != "" || len(r.MediaURLs) != 0 {
		t.Errorf("got %+v, want empty result", r)
	}
}

func TestParse_NoMedia(t *testing.T) {
	r := Parse("Hello world, no media here.")
	if r.Text != "Hello world, no media here." {
		t.Errorf("got %q, want original text", r.Text)
	}
	if len(r.MediaURLs) != 0 {
		t.Errorf("got %v, want no URLs", r.MediaURLs)
	}
}

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

func TestParse_VoiceAlias(t *testing.T) {
	r := Parse("Hello [[voice]]\nMEDIA: /tmp/voice.wav")
	if !r.AudioAsVoice {
		t.Error("expected audio_as_voice via [[voice]] alias")
	}
	if strings.Contains(r.Text, "[[voice]]") {
		t.Errorf("tag should be stripped: %s", r.Text)
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

func TestParse_BareFilename(t *testing.T) {
	r := Parse("MEDIA: image.png")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "image.png" {
		t.Errorf("got %v, want bare filename", r.MediaURLs)
	}
}

func TestParse_BareFilenameM4A(t *testing.T) {
	r := Parse("MEDIA: recording.m4a")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "recording.m4a" {
		t.Errorf("got %v, want bare filename", r.MediaURLs)
	}
}

func TestParse_InvalidMediaKept(t *testing.T) {
	r := Parse("MEDIA: not a valid path")
	if len(r.MediaURLs) != 0 {
		t.Errorf("got %v, want no URLs", r.MediaURLs)
	}
	if !strings.Contains(r.Text, "MEDIA:") {
		t.Errorf("invalid MEDIA: line should be kept in text")
	}
}

func TestParse_WindowsPath(t *testing.T) {
	r := Parse("MEDIA: C:\\Users\\test\\photo.jpg")
	if len(r.MediaURLs) != 1 {
		t.Errorf("got %v, want Windows path accepted", r.MediaURLs)
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

func TestParse_UnclosedBracket(t *testing.T) {
	r := Parse("Hello [[ not closed")
	if r.Text != "Hello [[ not closed" {
		t.Errorf("got %q, want text preserved", r.Text)
	}
	if r.AudioAsVoice {
		t.Error("expected no audio_as_voice")
	}
}

func TestParse_UNCPath(t *testing.T) {
	r := Parse("MEDIA: \\\\server\\share\\file.mp3")
	if len(r.MediaURLs) != 1 {
		t.Errorf("got %v, want UNC path accepted", r.MediaURLs)
	}
}

func TestParse_RelativePath(t *testing.T) {
	r := Parse("MEDIA: ./local/file.mp3")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "./local/file.mp3" {
		t.Errorf("got %v, want relative path", r.MediaURLs)
	}
}

func TestParse_TildePath(t *testing.T) {
	r := Parse("MEDIA: ~/music/song.mp3")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "~/music/song.mp3" {
		t.Errorf("got %v, want tilde path", r.MediaURLs)
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

func TestParse_EmptyPayload(t *testing.T) {
	r := Parse("MEDIA:   \ntext")
	if !strings.Contains(r.Text, "MEDIA:") {
		t.Errorf("MEDIA: with empty payload should be kept: %s", r.Text)
	}
}

// --- Rust parity: whitespace-only input ---

func TestParse_WhitespaceOnly(t *testing.T) {
	r := Parse("   \n\t  ")
	if r.Text != "" {
		t.Errorf("got %q, want empty text", r.Text)
	}
}

// --- Rust parity: MEDIA: with leading whitespace ---

func TestParse_LeadingWhitespace(t *testing.T) {
	r := Parse("  MEDIA: https://example.com/img.png")
	if len(r.MediaURLs) != 1 || r.MediaURLs[0] != "https://example.com/img.png" {
		t.Errorf("got %v, want URL with leading whitespace", r.MediaURLs)
	}
}

// --- Rust parity: likely local path drops line ---

func TestParse_LikelyLocalPathDropped(t *testing.T) {
	// Path that looks like a local path but has invalid chars — should still drop the line.
	r := Parse("text\nMEDIA: /some/weird path\nmore text")
	// The Rust impl drops the line via is_likely_local_path.
	if strings.Contains(r.Text, "MEDIA:") {
		t.Errorf("likely local path should be dropped: %s", r.Text)
	}
}

// --- Rust parity: collapse_whitespace ---

func TestCollapseWhitespace_MultipleNewlines(t *testing.T) {
	got := collapseWhitespace("a\n\n\nb")
	if got != "a\nb" {
		t.Errorf("got %q, want single newline", got)
	}
}

func TestCollapseWhitespace_MultipleSpaces(t *testing.T) {
	got := collapseWhitespace("a   b")
	if got != "a b" {
		t.Errorf("got %q, want single space", got)
	}
}

func TestCollapseWhitespace_TrailingSpaceBeforeNewline(t *testing.T) {
	got := collapseWhitespace("a   \nb")
	if got != "a\nb" {
		t.Errorf("got %q, want trailing space trimmed", got)
	}
}

// --- Rust parity: isBareFilename edge cases ---

func TestIsBareFilename_NoExtension(t *testing.T) {
	if isBareFilename("noext") {
		t.Error("expected false for no extension")
	}
}

func TestIsBareFilename_TooLongExt(t *testing.T) {
	if isBareFilename("file.abcdefghijk") {
		t.Error("expected false for 11-char extension")
	}
}

func TestIsBareFilename_PathSeparator(t *testing.T) {
	if isBareFilename("path/file.png") {
		t.Error("expected false for path with separator")
	}
}

func TestIsBareFilename_EmptyName(t *testing.T) {
	if isBareFilename(".png") {
		t.Error("expected false for empty name before dot")
	}
}
