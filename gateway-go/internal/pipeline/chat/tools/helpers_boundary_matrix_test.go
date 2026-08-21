package tools

import (
	"fmt"
	"testing"
	"unicode/utf8"
)

func TestBoundaryGenericTruncateRuneMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{name: "empty zero", text: "", max: 0, want: ""},
		{name: "ascii below", text: "ab", max: 3, want: "ab"},
		{name: "ascii exact", text: "abc", max: 3, want: "abc"},
		{name: "ascii over", text: "abcd", max: 3, want: "abc..."},
		{name: "Korean below", text: "가나", max: 3, want: "가나"},
		{name: "Korean exact", text: "가나다", max: 3, want: "가나다"},
		{name: "Korean over", text: "가나다라", max: 3, want: "가나다..."},
		{name: "emoji rune", text: "A📎B", max: 2, want: "A📎..."},
		{name: "newline rune", text: "a\nb", max: 2, want: "a\n..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.text, tt.max); got != tt.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.want)
			}
			if got := truncateRunes(tt.text, tt.max); !utf8.ValidString(got) {
				t.Fatalf("truncateRunes returned invalid UTF-8: %x", []byte(got))
			}
		})
	}
}

func TestBoundaryFormatBytesMatrix(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: -1, want: "-1 B"},
		{bytes: 0, want: "0 B"},
		{bytes: 1, want: "1 B"},
		{bytes: 999, want: "999 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1536, want: "1.5 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
		{bytes: 5 * 1024 * 1024, want: "5.0 MB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("bytes_%d", tt.bytes), func(t *testing.T) {
			if got := formatBytes(tt.bytes); got != tt.want {
				t.Fatalf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestBoundaryFirstLineMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "one line", text: "one", want: "one"},
		{name: "one line preserves edges", text: "  one  ", want: "  one  "},
		{name: "newline trims first", text: "  one  \ntwo", want: "one"},
		{name: "empty first", text: "\ntwo", want: ""},
		{name: "CR retained before LF then trimmed", text: "one\r\ntwo", want: "one"},
		{name: "multiple lines", text: "first\nsecond\nthird", want: "first"},
		{name: "unicode", text: "  첫 줄 📎  \n둘째", want: "첫 줄 📎"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.text); got != tt.want {
				t.Fatalf("firstLine(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
