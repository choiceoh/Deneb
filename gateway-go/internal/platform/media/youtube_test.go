package media

import (
	"testing"
)

func TestIsYouTubeURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://youtube.com/watch?v=dQw4w9WgXcQ", true},
		{"https://youtu.be/dQw4w9WgXcQ", true},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", true},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", true},
		{"이 영상 봐 https://youtu.be/dQw4w9WgXcQ 재밌어", true},
		{"https://example.com", false},
		{"not a url", false},
		{"", false},
		{"https://youtube.com/channel/UCxxx", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := IsYouTubeURL(tt.input); got != tt.want {
				t.Errorf("IsYouTubeURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractYouTubeURLs(t *testing.T) {
	text := "Check out https://youtu.be/abc12345678 and also https://www.youtube.com/watch?v=xyz12345678"
	urls := ExtractYouTubeURLs(text)
	if len(urls) != 2 {
		t.Fatalf("got %d URLs, want 2", len(urls))
	}
	if urls[0] != "https://youtu.be/abc12345678" {
		t.Errorf("urls[0] = %q, want youtu.be URL", urls[0])
	}
	if urls[1] != "https://www.youtube.com/watch?v=xyz12345678" {
		t.Errorf("urls[1] = %q, want youtube.com URL", urls[1])
	}
}

func TestCleanSubtitleText(t *testing.T) {
	raw := `WEBVTT
Kind: captions
Language: en

00:00:00.000 --> 00:00:02.000
Hello world

00:00:02.000 --> 00:00:04.000
Hello world

00:00:04.000 --> 00:00:06.000
<c>This is</c> a <b>test</b>

00:00:06.000 --> 00:00:08.000
Another line
`

	text := cleanSubtitleText(raw)
	if text == "" {
		t.Fatal("cleanSubtitleText returned empty string")
	}

	// Should not contain timestamps.
	if contains(text, "00:00") {
		t.Error("subtitle text contains timestamps")
	}
	// Should not contain WEBVTT header.
	if contains(text, "WEBVTT") {
		t.Error("subtitle text contains WEBVTT header")
	}
	// Should not contain HTML tags.
	if contains(text, "<c>") || contains(text, "<b>") {
		t.Error("subtitle text contains HTML tags")
	}
	// Should deduplicate "Hello world".
	lines := splitLines(text)
	helloCount := 0
	for _, l := range lines {
		if l == "Hello world" {
			helloCount++
		}
	}
	if helloCount != 1 {
		t.Errorf("'Hello world' appears %d times, want 1 (deduped)", helloCount)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		secs int
		want string
	}{
		{0, ""},
		{65, "1:05"},
		{3661, "1:01:01"},
		{120, "2:00"},
	}
	for _, tt := range tests {
		if got := formatDuration(tt.secs); got != tt.want {
			t.Errorf("formatDuration(%d) = %q, want %q", tt.secs, got, tt.want)
		}
	}
}

func TestFormatViewCount(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{500, "500회"},
		{15000, "1.5만회"},
		{1234567, "123.5만회"},
		{200000000, "2억회"},
	}
	for _, tt := range tests {
		if got := formatViewCount(tt.n); got != tt.want {
			t.Errorf("formatViewCount(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
