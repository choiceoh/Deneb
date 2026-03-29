package discord

import "testing"

func TestContainsKorean(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"설정 파일을 확인하고 있습니다", true},
		{"Thinking Process: Analyze the Request", false},
		{"", false},
		{"mixed 한국어 text", true},
		{"hello world", false},
	}
	for _, tt := range tests {
		if got := containsKorean(tt.input); got != tt.want {
			t.Errorf("containsKorean(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestCollapseNewlines(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"single line", "single line"},
		{"line1\nline2", "line1 line2"},
		{"line1\r\nline2\nline3", "line1 line2 line3"},
		{"", ""},
		{"no newlines here", "no newlines here"},
	}
	for _, tt := range tests {
		if got := collapseNewlines(tt.input); got != tt.want {
			t.Errorf("collapseNewlines(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFallbackThreadName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"코드 리뷰 해줘", "코드 리뷰 해줘"},
		{"first line\nsecond line", "first line"},
		{"", "New Thread"},
		{"   trimmed   ", "trimmed"},
	}
	for _, tt := range tests {
		if got := fallbackThreadName(tt.input); got != tt.want {
			t.Errorf("fallbackThreadName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateThreadName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"short", "short"},
		{"", "New Thread"},
		{"   ", "New Thread"},
		// 101 characters should be truncated.
		{string(make([]rune, 101)), string(make([]rune, 99)) + "…"},
	}
	for _, tt := range tests {
		got := truncateThreadName(tt.input)
		if got != tt.want {
			t.Errorf("truncateThreadName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNilSummarizerSafety(t *testing.T) {
	var s *Summarizer

	// ThreadTitle should return fallback.
	if got := s.ThreadTitle(nil, "hello"); got != "hello" {
		t.Errorf("nil Summarizer.ThreadTitle() = %q, want %q", got, "hello")
	}

	// ReasoningSummary should return empty.
	if got := s.ReasoningSummary(nil, "thinking text"); got != "" {
		t.Errorf("nil Summarizer.ReasoningSummary() = %q, want empty", got)
	}
}
