package chat

import (
	"strings"
	"testing"
)

func TestCleanSessionTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "세금계산서 정리", "세금계산서 정리"},
		{"strips 제목 prefix", "제목: 오늘 일정 요약", "오늘 일정 요약"},
		{"strips wrapping quotes", "\"메일 분석 결과\"", "메일 분석 결과"},
		{"strips smart quotes", "“프로젝트 현황”", "프로젝트 현황"},
		{"first line only", "회의록 정리\n추가 설명은 무시", "회의록 정리"},
		{"trailing period", "예산 검토.", "예산 검토"},
		{"collapses whitespace", "  탑솔라   계약   검토  ", "탑솔라 계약 검토"},
		{"empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanSessionTitle(c.in); got != c.want {
				t.Errorf("cleanSessionTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCleanSessionTitle_CapsLength(t *testing.T) {
	long := strings.Repeat("가", 100)
	got := cleanSessionTitle(long)
	if n := len([]rune(got)); n > sessionTitleLabelCap {
		t.Errorf("title rune length = %d, want <= %d", n, sessionTitleLabelCap)
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"a\nb":   "a",
		"a\r\nb": "a",
		"single": "single",
		"":       "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapRunes(t *testing.T) {
	if got := capRunes("hello", 3); got != "hel" {
		t.Errorf("capRunes truncate = %q, want %q", got, "hel")
	}
	// CJK runes must be counted as single runes, not bytes.
	if got := capRunes("한국어제목", 3); got != "한국어" {
		t.Errorf("capRunes CJK = %q, want %q", got, "한국어")
	}
	if got := capRunes("  short  ", 50); got != "short" {
		t.Errorf("capRunes trims = %q, want %q", got, "short")
	}
}
