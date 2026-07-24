package evenapi

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanForG2StripsMarkdownAndURLs(t *testing.T) {
	in := "## 다음 일정\n\n**14:00** 회의\n- 준비물 확인\n자세한 링크: https://example.com/x\n`code`"
	got := CleanForG2(in)
	if got == "" {
		t.Fatal("empty clean result")
	}
	for _, bad := range []string{"##", "**", "https://", "`"} {
		if strings.Contains(got, bad) {
			t.Fatalf("cleaned text still contains %q: %q", bad, got)
		}
	}
	if !strings.Contains(got, "14:00") {
		t.Fatalf("lost schedule content: %q", got)
	}
}

func TestCleanForG2Truncates(t *testing.T) {
	long := strings.Repeat("가", MaxG2Runes+50)
	got := CleanForG2(long)
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis truncate, got %q", got)
	}
	if utf8.RuneCountInString(got) != MaxG2Runes {
		t.Fatalf("rune count = %d, want %d", utf8.RuneCountInString(got), MaxG2Runes)
	}
}
