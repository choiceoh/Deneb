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

func TestStripThinkingHeaders(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{
			"Thinking Process:\nAnalyze the Request:\nThe user wants to fix a bug",
			"The user wants to fix a bug",
		},
		{
			"Thinking Process: some reasoning here",
			"some reasoning here",
		},
		{
			"Analyze the Request:\nLet me think about this",
			"about this",
		},
		{
			"The user wants to add a login feature",
			"The user wants to add a login feature",
		},
		{
			"  Thinking Process:\n  actual content  ",
			"actual content",
		},
		{"", ""},
		{"Thinking Process:", ""},
	}
	for _, tt := range tests {
		if got := stripThinkingHeaders(tt.input); got != tt.want {
			t.Errorf("stripThinkingHeaders(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
