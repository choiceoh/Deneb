package wiki

import "testing"

// TestUniqueProjectIn_OwnCompanyClientNeverResolves: our own name appears in
// every internal subject, so a project whose client is 탑솔라 must not absorb
// them. Live damage: 5 of 7 title-signal refiles in 2026-08 landed unrelated
// "[탑솔라(주)] …" mails in the one project carrying client: 탑솔라.
func TestUniqueProjectIn_OwnCompanyClientNeverResolves(t *testing.T) {
	projects := []ProjectRef{
		{Path: "프로젝트/pl1-cny-dev-001/대표.md", Name: "충남 영농형 태양광", Client: "탑솔라"},
		{Path: "프로젝트/pl2-asf-epc-001/대표.md", Name: "안성 스타필드 주차장 태양광", Client: "신세계"},
	}

	if ref, ok := uniqueProjectIn(normalizeTitleKey("[탑솔라(주)] 임동중흥S클래스 검토사항"), projects); ok {
		t.Errorf("own-company subject resolved to %s", ref.Path)
	}
	ref, ok := uniqueProjectIn(normalizeTitleKey("[탑솔라(주)] 안성 스타필드 주차장 태양광 사업성 검토"), projects)
	if !ok || ref.Path != "프로젝트/pl2-asf-epc-001/대표.md" {
		t.Errorf("specific project title = %+v ok=%v, want 안성 스타필드", ref, ok)
	}
}
