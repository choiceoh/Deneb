package memory

import "testing"

// Go's \b is an ASCII word boundary, so a pattern that ends one right after a
// Hangul alternative can never match: a Hangul syllable is not an ASCII word
// character. The forward and correction leads did exactly that, which killed
// the two most common Korean ways to state a standing preference or a
// correction while their English twins kept working.
func TestDirectMemoryLeadMatchesKoreanCommandOpenings(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"앞으로는 보고서를 표로 만들어줘", "forward"},
		{"앞으로 짧게 답해줘", "forward"},
		{"다음부터 존댓말로", "forward"},
		{"아니야, 그건 틀렸어", "correction"},
		{"아니요 제 이름은 그게 아니에요", "correction"},
		{"정정할게 — 미팅은 화요일이야", "correction"},
		{"수정해줘, 담당자는 박 차장이야", "correction"},
		// The English twins must keep working.
		{"from now on use metric units", "forward"},
		{"remember that I prefer short answers", "remember"},
		{"기억해줘: 나는 커피를 안 마신다", "remember"},
		{"내 취향은 짧은 답변이야, 기억해줘.", "trailing_remember"},
		{"기억에서 그 항목 지워줘", "forget"},
	}
	for _, tc := range cases {
		got, found := DirectMemoryLead(tc.message)
		if !found || got != tc.want {
			t.Errorf("DirectMemoryLead(%q) = %q,%v; want %q", tc.message, got, found, tc.want)
		}
	}
}

// Ordinary conversation must still not read as a memory command — the lead set
// is deliberately broad, not unbounded.
func TestDirectMemoryLeadIgnoresPlainConversation(t *testing.T) {
	for _, msg := range []string{
		"오늘 일정 알려줘",
		"이 메일 요약해줘",
		"프로젝트 진행 상황이 어때?",
		"what is on my calendar today",
	} {
		if got, found := DirectMemoryLead(msg); found {
			t.Errorf("DirectMemoryLead(%q) = %q; want no lead", msg, got)
		}
	}
}

// A lead alone is not a miss: the miss ledger only wants turns the binding
// grammar routed AWAY from memory.
func TestDirectMemoryMissSkipsTurnsTheGrammarBound(t *testing.T) {
	msg := "앞으로는 보고서를 표로 만들어줘"
	if _, found := DirectMemoryMissFor(msg, &Induced{Route: RouteMemory}); found {
		t.Error("a turn the grammar bound to memory was recorded as a miss")
	}
	if _, found := DirectMemoryMissFor(msg, &Induced{Route: RouteDiaryOnly}); !found {
		t.Error("a lead-bearing turn routed away from memory was not recorded")
	}
}
