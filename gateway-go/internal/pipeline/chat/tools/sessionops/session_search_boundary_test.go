package sessionops

import (
	"testing"
	"unicode/utf8"
)

func TestBoundarySessionSearchTokenNormalizationMatrix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "---", want: ""},
		{in: "-Project_", want: "project"},
		{in: "태양광에서", want: "태양광"},
		{in: "프로젝트를", want: "프로젝트"},
		{in: "찾아주세요", want: "찾아주세요"},
		{in: "CI", want: "ci"},
		{in: "PR", want: "pr"},
	}
	for _, tt := range tests {
		if got := normalizeSessionSearchToken(tt.in); got != tt.want {
			t.Fatalf("normalizeSessionSearchToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBoundarySessionSearchSignalMatrix(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{token: "", want: false},
		{token: "a", want: false},
		{token: "ab", want: false},
		{token: "abc", want: true},
		{token: "ai", want: true},
		{token: "ci", want: true},
		{token: "pr", want: true},
		{token: "가", want: false},
		{token: "가나", want: true},
		{token: "12", want: true},
	}
	for _, tt := range tests {
		if got := isSessionSearchSignalToken(tt.token); got != tt.want {
			t.Fatalf("isSessionSearchSignalToken(%q) = %v, want %v", tt.token, got, tt.want)
		}
	}
}

func TestBoundaryTruncateUsesRuneBoundary(t *testing.T) {
	tests := []struct {
		text string
		max  int
		want string
	}{
		{text: "", max: 0, want: ""},
		{text: "abc", max: 3, want: "abc"},
		{text: "abcd", max: 3, want: "abc..."},
		{text: "가나다라", max: 3, want: "가나다..."},
		{text: "A📎B", max: 2, want: "A📎..."},
	}
	for _, tt := range tests {
		got := Truncate(tt.text, tt.max)
		if got != tt.want || !utf8.ValidString(got) {
			t.Fatalf("Truncate(%q,%d) = %q", tt.text, tt.max, got)
		}
	}
}
