package recall

import "testing"

// TestMatchCitedPaths_UsesPageTitleAndProjectCode: a mail analysis is filed
// under its Gmail message id, so an answer about that mail contains nothing
// path-shaped — 61% of injected client paths in the 30-day ledger are uuid or
// hash filenames, which is the main reason the ledger recorded 2 cites in 41
// days. The page's own title (the mail subject) and the frozen project code are
// what an answer actually repeats.
func TestMatchCitedPaths_UsesPageTitleAndProjectCode(t *testing.T) {
	identity := func(p string) (string, string) {
		switch p {
		case "프로젝트/nde-sun-cbl-001/메일분석/2026080316584904182860@sunkean.com.md":
			return "Re: [Namdoeco Energy] MC4 커넥터 단가 회신", ""
		case "프로젝트/pl2-kia-epc-001/대표.md":
			return "기아 오토랜드 화성 태양광", "pl2-kia-epc-001"
		case "인물/백창선.md":
			return "백창선", ""
		case "업무/현황.md":
			return "현황", "" // generic — must never match
		}
		return "", ""
	}
	injected := []string{
		"프로젝트/nde-sun-cbl-001/메일분석/2026080316584904182860@sunkean.com.md",
		"프로젝트/pl2-kia-epc-001/대표.md",
		"인물/백창선.md",
		"업무/현황.md",
	}

	answer := "MC4 커넥터 단가 회신 기준으로 정리하면, pl2-kia-epc-001 건은 별개입니다. 현황은 아래와 같습니다."
	got := matchCitedPaths(answer, injected, identity)

	want := map[string]bool{
		"프로젝트/nde-sun-cbl-001/메일분석/2026080316584904182860@sunkean.com.md": false,
		"프로젝트/pl2-kia-epc-001/대표.md":                                      true,
	}
	seen := map[string]bool{}
	for _, p := range got {
		seen[p] = true
	}
	if !seen["프로젝트/pl2-kia-epc-001/대표.md"] {
		t.Error("project code in the answer did not count as a citation")
	}
	if seen["업무/현황.md"] {
		t.Error("generic title '현황' counted as a citation")
	}
	if seen["인물/백창선.md"] {
		t.Error("page never named in the answer counted as a citation")
	}
	_ = want

	// The mail's subject fragment appears verbatim → its page counts.
	subjectAnswer := "Re: [Namdoeco Energy] MC4 커넥터 단가 회신 메일에 단가가 있습니다."
	got = matchCitedPaths(subjectAnswer, injected, identity)
	if len(got) != 1 || got[0] != "프로젝트/nde-sun-cbl-001/메일분석/2026080316584904182860@sunkean.com.md" {
		t.Errorf("subject-titled mail not cited: %v", got)
	}

	// Without a resolver the matcher behaves exactly as before.
	if got := matchCitedPaths(subjectAnswer, injected, nil); len(got) != 0 {
		t.Errorf("nil resolver changed behaviour: %v", got)
	}
}
