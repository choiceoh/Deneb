package chat

import "testing"

// cleanThinkingSummaryLine must reduce a tiny model's output to a single chip
// line: first non-empty line, wrapper/quote/markdown punctuation stripped,
// trailing sentence punctuation dropped, and capped in length.
func TestCleanThinkingSummaryLine(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"strips surrounding quotes", "\"지난 거래 대조 중\"", "지난 거래 대조 중"},
		{"drops trailing period", "발신자 확인 중.", "발신자 확인 중"},
		{"takes first non-empty line", "\n- 일정 정리 중\n다음 줄은 버린다", "일정 정리 중"},
		{"strips markdown bullet", "* 메일 3건 대조 중", "메일 3건 대조 중"},
		{"blank input yields empty", "  \n  \t ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanThinkingSummaryLine(c.in); got != c.want {
				t.Errorf("cleanThinkingSummaryLine(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// The line must be capped to thinkingSummaryMaxRunes runes (rune-safe for Korean).
func TestCleanThinkingSummaryLineCaps(t *testing.T) {
	long := ""
	for i := 0; i < thinkingSummaryMaxRunes+20; i++ {
		long += "가"
	}
	got := cleanThinkingSummaryLine(long)
	if n := len([]rune(got)); n > thinkingSummaryMaxRunes {
		t.Fatalf("line length = %d runes, want <= %d", n, thinkingSummaryMaxRunes)
	}
}
