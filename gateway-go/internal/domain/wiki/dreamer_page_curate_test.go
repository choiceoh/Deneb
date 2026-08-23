package wiki

import (
	"strings"
	"testing"
)

func TestCollapseDuplicateBullets(t *testing.T) {
	body := "## 현재 상태\n- 견적 송부\n- 견적 송부\n- 회신 대기\n\n## 핵심 숫자\n- 858백만\n- 858백만\n"
	got := collapseDuplicateBullets(body)
	if strings.Count(got, "견적 송부") != 1 || strings.Count(got, "858백만") != 1 {
		t.Fatalf("duplicates survived:\n%s", got)
	}
	if !strings.Contains(got, "회신 대기") {
		t.Fatalf("unique bullet dropped:\n%s", got)
	}
}

func TestCuratePageBody_RejectsTinyAndCollapse(t *testing.T) {
	// Tiny: one short dup against a large unique body — reduction under 5%.
	small := strings.Repeat("고유한 본문 라인입니다 번호를 붙입니다.\n", 50) + "- 같은 불릿입니다 충분히 길게\n- 같은 불릿입니다 충분히 길게\n"
	if _, ok := curatePageBody(small); ok {
		t.Fatal("tiny cleanup must be rejected")
	}

	// Collapse: almost the whole page is the same bullet.
	wiped := strings.Repeat("- 같은 불릿입니다 충분히 길게 반복\n", 400)
	if _, ok := curatePageBody(wiped); ok {
		t.Fatal("≥95% collapse must be rejected")
	}

	var b strings.Builder
	b.WriteString("## 현재 상태\n")
	for i := 0; i < 80; i++ {
		b.WriteString("- 중복 상태 줄입니다 충분히 긴 문장\n")
	}
	for i := 0; i < 80; i++ {
		b.WriteString("- 고유 상태 ")
		b.WriteString(strings.Repeat("가", 20))
		b.WriteByte(byte('A' + i%26))
		b.WriteString("\n")
	}
	cleaned, ok := curatePageBody(b.String())
	if !ok {
		t.Fatal("mid-size dup collapse must apply")
	}
	if strings.Count(cleaned, "중복 상태 줄입니다") != 1 {
		t.Fatalf("expected one copy of the repeated bullet:\n%s", cleaned)
	}
}
