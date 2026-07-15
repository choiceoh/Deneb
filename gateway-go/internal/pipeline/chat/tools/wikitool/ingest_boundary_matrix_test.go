package wikitool

import (
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBoundaryNormalizeSourceURLMatrix(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		err  string
	}{
		{name: "empty", raw: "", err: "빈 URL"},
		{name: "spaces", raw: "  ", err: "빈 URL"},
		{name: "unsupported ftp", raw: "ftp://example.com/a", err: "지원하지 않는 스킴"},
		{name: "unsupported relative", raw: "/relative", err: "지원하지 않는 스킴"},
		{name: "HTTP preserved", raw: "http://EXAMPLE.COM/a", want: "http://example.com/a"},
		{name: "HTTPS host lower", raw: "https://EXAMPLE.COM/A", want: "https://example.com/A"},
		{name: "fragment removed", raw: "https://example.com/a#section", want: "https://example.com/a"},
		{name: "utm removed", raw: "https://example.com/a?utm_source=x&keep=y", want: "https://example.com/a?keep=y"},
		{name: "query sorted", raw: "https://example.com/a?z=2&a=1", want: "https://example.com/a?a=1&z=2"},
		{name: "youtu be", raw: "https://youtu.be/abc123?t=30", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube watch", raw: "https://www.youtube.com/watch?v=abc123&list=PL", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube shorts", raw: "https://youtube.com/shorts/abc123?feature=share", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube embed", raw: "https://m.youtube.com/embed/abc123", want: "https://www.youtube.com/watch?v=abc123"},
		{name: "youtube live", raw: "https://music.youtube.com/live/abc123", want: "https://www.youtube.com/watch?v=abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeSourceURL(tt.raw)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("normalizeSourceURL = %q, %v", got, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("normalizeSourceURL = %q, %v, want %q", got, err, tt.want)
			}
		})
	}
}

func TestBoundaryYouTubeVideoIDMatrix(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "https://youtube.com/watch?v=watch-id", want: "watch-id"},
		{raw: "https://www.youtube.com/watch?v=www-id", want: "www-id"},
		{raw: "https://m.youtube.com/watch?v=mobile-id", want: "mobile-id"},
		{raw: "https://music.youtube.com/watch?v=music-id", want: "music-id"},
		{raw: "https://youtube.com/shorts/short-id", want: "short-id"},
		{raw: "https://youtube.com/embed/embed-id/more", want: "embed-id"},
		{raw: "https://youtube.com/live/live-id", want: "live-id"},
		{raw: "https://youtu.be/share-id", want: "share-id"},
		{raw: "https://youtu.be/share-id/extra", want: "share-id"},
		{raw: "https://example.com/watch?v=no", want: ""},
		{raw: "https://notyoutube.com/watch?v=no", want: ""},
		{raw: "https://youtube.com/watch", want: ""},
		{raw: "https://youtu.be/", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			u, err := url.Parse(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := youtubeVideoID(u); got != tt.want {
				t.Fatalf("youtubeVideoID(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestBoundarySlugifyTitleMatrix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: "자료"},
		{name: "punctuation only", in: "!!!", want: "자료"},
		{name: "ascii lower", in: "Hello World", want: "hello-world"},
		{name: "Korean", in: "태양광 계약 검토", want: "태양광-계약-검토"},
		{name: "mixed", in: "2026 Q3 태양광", want: "2026-q3-태양광"},
		{name: "collapse punctuation", in: "a --- b///c", want: "a-b-c"},
		{name: "trim dashes", in: " -- title -- ", want: "title"},
		{name: "underscore folds", in: "one_two", want: "one-two"},
		{name: "emoji folds", in: "report 📎 final", want: "report-final"},
		{name: "Hangul jamo", in: "ㄱㄴ test", want: "ㄱㄴ-test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slugifyTitle(tt.in); got != tt.want {
				t.Fatalf("slugifyTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	long := strings.Repeat("가", ingestSlugMaxRunes+20)
	got := slugifyTitle(long)
	if utf8.RuneCountInString(got) != ingestSlugMaxRunes || strings.Contains(got, "...") {
		t.Fatalf("long slug length=%d value=%q", utf8.RuneCountInString(got), got)
	}
}
