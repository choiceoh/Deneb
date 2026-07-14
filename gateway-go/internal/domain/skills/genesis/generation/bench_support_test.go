package generation

import (
	"strings"
	"testing"
)

// BenchAdmissibility must mirror the production pipeline: a clean skill
// passes with zero issues, a vague one collects the same specificity issues
// Evaluate would, a skip verdict reports skipped, and garbage errors.
func TestBenchAdmissibilityClassifiesCleanVagueSkipAndInvalidInput(t *testing.T) {
	clean := `{"skip": false, "skill": {"name": "mail-to-wiki-sync", "category": "productivity",
		"description": "발주 메일을 위키 거래처 페이지에 반영. Use when: 미반영 발주 메일 정리. NOT for: 단순 열람.",
		"body": "# 메일-위키 동기화\n\n## When to Use\n발주 메일이 쌓였을 때.\n\n## Procedure\n1. ` + "`gmail_search`" + `로 미반영 메일을 찾는다.\n2. 위키 페이지에 append 한다.\n3. 재조회로 반영을 확인한다.\n\n## Pitfalls\n중복 페이지 주의.\n\n## Verification\n위키 재조회. ` + strings.Repeat("상세 절차 설명. ", 30) + `"}}`
	skipped, issues, err := BenchAdmissibility(clean)
	if err != nil || skipped || len(issues) != 0 {
		t.Fatalf("clean skill: skipped=%v issues=%v err=%v", skipped, issues, err)
	}

	vague := `{"skip": false, "skill": {"name": "be-careful", "category": "productivity",
		"description": "맥락을 잘 살펴라",
		"body": "맥락을 잘 살펴서 신중하게 작업하라."}}`
	skipped, issues, err = BenchAdmissibility(vague)
	if err != nil || skipped || len(issues) == 0 {
		t.Fatalf("vague skill must collect issues: skipped=%v issues=%v err=%v", skipped, issues, err)
	}

	skipped, issues, err = BenchAdmissibility(`{"skip": true, "reason": "일회성"}`)
	if err != nil || !skipped || len(issues) != 0 {
		t.Fatalf("skip verdict: skipped=%v issues=%v err=%v", skipped, issues, err)
	}

	if _, _, err = BenchAdmissibility("완전한 산문 응답이라 JSON이 아닙니다"); err == nil {
		t.Fatal("garbage must error, not silently pass")
	}
}
