package chat

import "testing"

func TestStripReasoningLeak(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain prose untouched", "안녕하세요, 무엇을 도와드릴까요?", "안녕하세요, 무엇을 도와드릴까요?"},
		{"empty", "", ""},
		{"markdown link untouched", "자세히는 [문서](https://x)를 보세요", "자세히는 [문서](https://x)를 보세요"},
		{"bracket thinking block removed", "[thinking]사용자는 인사를 했다[/thinking]안녕하세요!", "안녕하세요!"},
		{"angle think block removed", "<think>\n추론...\n</think>\n실제 답변", "실제 답변"},
		{"angle thinking block removed", "<thinking>reason</thinking>답", "답"},
		{"standalone open marker removed", "[thinking]답변만 남김", "답변만 남김"},
		{"standalone close marker removed", "답변[/thinking]", "답변"},
		{"case insensitive", "<THINK>x</THINK>네", "네"},
		{"multiline block whole-removed", "<think>line1\nline2\nline3</think>\n\n결론", "결론"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripReasoningLeak(c.in)
			// buildSyncResult trims; mirror that for the final-answer assertions.
			if trimmed := trimSpace(got); trimmed != c.want && got != c.want {
				t.Fatalf("stripReasoningLeak(%q) = %q (trimmed %q), want %q", c.in, got, trimmed, c.want)
			}
		})
	}
}

// trimSpace is a tiny local helper so the test mirrors buildSyncResult's
// strings.TrimSpace without importing strings just for the test table.
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\n' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\n' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
