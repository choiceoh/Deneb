package groupware

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeApprovalComment(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single line controls and html delimiters",
			in:   "  첫 줄\n\t<script>\u0000 반려  사유\r\n",
			want: "첫 줄 script 반려 사유",
		},
		{
			name: "blank controls",
			in:   " \n\t\u0000\r ",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeApprovalComment(tt.in); got != tt.want {
				t.Fatalf("SanitizeApprovalComment(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeApprovalCommentTruncatesKoreanByRune(t *testing.T) {
	got := SanitizeApprovalComment(strings.Repeat("한", MaxApprovalCommentRunes+100))
	if count := utf8.RuneCountInString(got); count != MaxApprovalCommentRunes {
		t.Fatalf("rune count = %d, want %d", count, MaxApprovalCommentRunes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("sanitized comment is not valid UTF-8")
	}
}
