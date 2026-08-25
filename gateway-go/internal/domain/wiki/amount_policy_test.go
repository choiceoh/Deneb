package wiki

import "testing"

// The advisory contract: a VAT-inclusive-only amount on a wiki-authored
// surface is flagged; a supply-price companion, a quoted mail analysis, or a
// non-project page exempts it. Read-only — the scan must not edit anything.
func TestScanAmountPolicyViolations(t *testing.T) {
	s, _ := newVerifyStore(t)
	writePageT(t, s, "프로젝트/nde-x/대표.md", "위반", "프로젝트",
		"1~5월 누적 78억원(VAT포함) 규모의 납품.")
	writePageT(t, s, "프로젝트/kia-x/대표.md", "병기", "프로젝트",
		"견적: 공급가액 6.51억원(VAT포함 7.16억원).")
	writePageT(t, s, "프로젝트/kia-x/메일분석/a.md", "인용", "프로젝트",
		"원문: 견적 7.16억(VAT 포함)을 송부드립니다.")
	writePageT(t, s, "업무/경비.md", "비대상", "업무",
		"접대비 500,000원 (VAT포함) 결재.")

	hits := s.ScanAmountPolicyViolations(10)
	if len(hits) != 1 {
		t.Fatalf("hits = %+v, want exactly the 대표.md violation", hits)
	}
	if hits[0].Path != "프로젝트/nde-x/대표.md" || hits[0].Snippet == "" {
		t.Fatalf("unexpected hit: %+v", hits[0])
	}

	// The violating page is untouched — advisory means advisory.
	page, err := s.ReadPage("프로젝트/nde-x/대표.md")
	if err != nil || page == nil || page.Body != "1~5월 누적 78억원(VAT포함) 규모의 납품." {
		t.Fatalf("scan mutated the page: %+v err=%v", page, err)
	}
}

// A frontmatter summary alone can carry the violation (nde-tge-cbl-001 did).
func TestScanAmountPolicyViolationsSeesSummary(t *testing.T) {
	s, _ := newVerifyStore(t)
	if err := s.WritePage("프로젝트/y/대표.md", &Page{
		Meta: Frontmatter{
			ID: "y", Title: "y", Category: "프로젝트",
			Summary: "누적 78억원(VAT포함) 납품",
		},
		Body: "본문에는 금액 없음",
	}); err != nil {
		t.Fatal(err)
	}
	if hits := s.ScanAmountPolicyViolations(10); len(hits) != 1 {
		t.Fatalf("summary violation missed: %+v", hits)
	}
}
