package chat

import "testing"

// Adoption-miss heuristic: markdown tables and long bullet runs are the shapes
// the authoring contract says should be cards; short prose and short lists are not.
func TestLooksStructuredWithoutCard(t *testing.T) {
	if !looksStructuredWithoutCard("| 항목 | 상태 |\n|---|---|\n| a | b |") {
		t.Fatal("markdown table must count as structured")
	}
	long := "서두 설명 문장입니다. "
	for i := 0; i < 70; i++ {
		long += "채움 문장. "
	}
	long += "\n- 하나\n- 둘\n- 셋\n- 넷\n- 다섯\n"
	if !looksStructuredWithoutCard(long) {
		t.Fatal("5+ bullets in a long answer must count as structured")
	}
	if looksStructuredWithoutCard("짧은 답변입니다.\n- 하나\n- 둘") {
		t.Fatal("short prose with two bullets must not count")
	}
}
