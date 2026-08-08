package wiki

import (
	"strings"
	"testing"
)

// The 2026-08-08 repair sweep's real before/after pairs are the contract: every
// pre-repair title violates, every repaired (or rule-legitimate) title passes.
func TestLintProjectRepTitle(t *testing.T) {
	violating := []string{
		"완도 관산포 태양광 — 현재 현안 (송전선로 + SK 협상)",             // status tail
		"아르고에너지(Argo Energy) NDA 송부 – 태양광 사업 전략적 협력 검토", // event + trailing stage word
		"중부도시가스 해남 200MW EPC 기회",                        // trailing stage word
		"SunKean 솔라케이블 2,240km + MC4 커넥터 협의",            // trailing stage word
		"산월풍력 EPC 입찰", // trailing stage word
		"세창스틸 1,2공장 태양광 — 6/29 견적 재송부 (재검토)", // status tail + date (the documented incident)
		"기아 화성 태양광 2026.06.15 RFx",           // year token
	}
	for _, title := range violating {
		if vs := LintProjectRepTitle(title); len(vs) == 0 {
			t.Errorf("expected violation for %q", title)
		}
	}

	clean := []string{
		"완도 관산포 태양광",
		"기아 오토랜드 화성 태양광",
		"중부도시가스 해남 200MW EPC",
		"SunKean 솔라케이블 2,240km + MC4 커넥터",   // comma numbers are not dates
		"비금도 154kV 해저케이블 (ZTT) — 비금 솔라 현장",  // site tail is legitimate (현장/지역 보존)
		"영광 한빛 BESS (80MW/480MWh, 한빛에너지저장)", // slash tech tokens are not dates
		"세창스틸 1,2공장 태양광",
		"트리나솔라 2026년 모듈 공급 계약", // vintage year is part of the name (live corpus)
		"",
	}
	for _, title := range clean {
		if vs := LintProjectRepTitle(title); len(vs) != 0 {
			t.Errorf("false positive for %q: %v", title, vs)
		}
	}
}

// Violations surface as advisory verify findings for rep pages only, capped.
func TestDetectTitleRuleViolationsAdvisoryAndScoped(t *testing.T) {
	entries := map[string]IndexEntry{
		"프로젝트/pl2-a-epc-001/대표.md": {Title: "A 태양광 입찰"},
		"프로젝트/pl2-a-epc-001/로그.md": {Title: "A 진행 로그 검토"}, // not a rep page — ignored
		"업무/시장-검토.md":              {Title: "구리값 검토"},     // not a project — ignored
		"프로젝트/pl2-b-epc-001/대표.md": {Title: "B 루프탑 태양광"},  // clean
	}
	findings := detectTitleRuleViolations(entries)
	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want exactly the rep-page violation", findings)
	}
	f := findings[0]
	if f.Type != "title_rule" || f.PageA != "프로젝트/pl2-a-epc-001/대표.md" || f.Fix != nil {
		t.Errorf("finding = %+v, want advisory title_rule on the rep page", f)
	}
	if !strings.Contains(f.Detail, "입찰") {
		t.Errorf("detail should name the offending word, got %q", f.Detail)
	}
}
