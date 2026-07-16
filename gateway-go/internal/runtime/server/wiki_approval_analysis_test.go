package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

func approvalWikiServer(t *testing.T) (*Server, *wiki.Store) {
	t.Helper()
	s, ws := discardServer(t)
	// A known project so UniqueProjectInText can anchor the approval title.
	if err := ws.WritePage(wiki.RepPagePath("완도 관산포"), wiki.NewPage("완도 관산포", "프로젝트", nil)); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return s, ws
}

func TestLogApprovalAnalysisToWiki_AppendsGistOnceToProjectLog(t *testing.T) {
	s, ws := approvalWikiServer(t)
	rec := &groupware.ApprovalAnalysisRecord{
		DocID:       "99172",
		Title:       "완도 관산포 프로젝트 관련 금전대여의 건",
		Drafter:     "공명한",
		Importance:  "attention",
		ProjectFile: true,
		Analysis:    "요지: 금전대여 승인 요청\nIMPORTANCE: attention\nPROJECT_FILE: yes\n핵심: 대여금 3억",
	}
	s.logApprovalAnalysisToWiki(rec, "")

	page, err := ws.ReadPage(wiki.LogPagePath("완도 관산포"))
	if err != nil || page == nil {
		t.Fatalf("log page: %v", err)
	}
	if !strings.Contains(page.Body, "결재 | 완도 관산포 프로젝트 관련 금전대여의 건") {
		t.Fatalf("결재 op missing:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "요지: 금전대여 승인 요청") {
		t.Fatal("analysis gist missing")
	}
	if strings.Contains(page.Body, "IMPORTANCE:") || strings.Contains(page.Body, "PROJECT_FILE:") {
		t.Fatal("machine trailers should be dropped from the gist")
	}

	rep, err := ws.ReadPage(wiki.RepPagePath("완도 관산포"))
	if err != nil || rep == nil {
		t.Fatalf("rep page: %v", err)
	}
	if !strings.Contains(rep.Body, "결재: 완도 관산포 프로젝트 관련 금전대여의 건") {
		t.Fatalf("현재 상태 bullet missing:\n%s", rep.Body)
	}

	// Re-log (force rerun) must not duplicate the section.
	s.logApprovalAnalysisToWiki(rec, "")
	page, _ = ws.ReadPage(wiki.LogPagePath("완도 관산포"))
	if got := strings.Count(page.Body, "ref=approval:99172"); got != 1 {
		t.Fatalf("expected 1 marker, got %d", got)
	}
	rep, _ = ws.ReadPage(wiki.RepPagePath("완도 관산포"))
	if got := strings.Count(rep.Body, "approval:99172"); got != 1 {
		t.Fatalf("expected 1 status ref, got %d\n%s", got, rep.Body)
	}
}

func TestLogApprovalAnalysisToWiki_SkipsWhenProjectFileFalse(t *testing.T) {
	s, ws := approvalWikiServer(t)
	s.logApprovalAnalysisToWiki(&groupware.ApprovalAnalysisRecord{
		DocID:       "99",
		Title:       "완도 관산포 관련 다과비",
		Importance:  "routine",
		ProjectFile: false,
		Analysis:    "요지: 다과비\nPROJECT_FILE: no",
	}, "")
	if page, _ := ws.ReadPage(wiki.LogPagePath("완도 관산포")); page != nil && strings.Contains(page.Body, "다과비") {
		t.Fatal("ProjectFile=false must not land in the project log")
	}
	if page, _ := ws.ReadPage(wiki.RepPagePath("완도 관산포")); page != nil && strings.Contains(page.Body, "다과비") {
		t.Fatal("ProjectFile=false must not land in 현재 상태")
	}
}

func TestLogApprovalAnalysisToWiki_MatchesProjectInBody(t *testing.T) {
	s, ws := approvalWikiServer(t)
	s.logApprovalAnalysisToWiki(&groupware.ApprovalAnalysisRecord{
		DocID:       "42",
		Title:       "기자재 구매 품의의 건",
		Importance:  "routine",
		ProjectFile: true,
		Analysis:    "요지: 모듈 발주\nPROJECT_FILE: yes",
	}, "현장: 완도 관산포 인버터 교체 관련 발주서")

	page, err := ws.ReadPage(wiki.LogPagePath("완도 관산포"))
	if err != nil || page == nil || !strings.Contains(page.Body, "기자재 구매") {
		t.Fatalf("body-matched project should get the log: %v\n%v", err, page)
	}
}

func TestLogApprovalAnalysisToWiki_SkipsWithoutUniqueProject(t *testing.T) {
	s, ws := approvalWikiServer(t)
	s.logApprovalAnalysisToWiki(&groupware.ApprovalAnalysisRecord{
		DocID:       "1",
		Title:       "다과비 추가 구입금액 지출 품의의 건",
		ProjectFile: true,
		Analysis:    "요지: 다과비\nPROJECT_FILE: yes",
	}, "")
	if page, _ := ws.ReadPage(wiki.LogPagePath("완도 관산포")); page != nil && strings.Contains(page.Body, "다과비") {
		t.Fatal("unrelated approval must not land in the project log")
	}
}

func TestNormalizeApprovalProjectFile(t *testing.T) {
	if !normalizeApprovalProjectFile("요지\nPROJECT_FILE: yes\n") {
		t.Fatal("yes must be true")
	}
	if !normalizeApprovalProjectFile("PROJECT_FILE: Yes — keep") {
		t.Fatal("Yes prefix must be true")
	}
	if normalizeApprovalProjectFile("PROJECT_FILE: no") {
		t.Fatal("no must be false")
	}
	if normalizeApprovalProjectFile("IMPORTANCE: urgent\n요지") {
		t.Fatal("missing PROJECT_FILE must be false")
	}
	if normalizeApprovalProjectFile("") {
		t.Fatal("empty must be false")
	}
}

func TestApprovalAnalysisExcerptBounds(t *testing.T) {
	long := strings.Repeat("가나다라마바사아자차", 100)
	out := approvalAnalysisExcerpt(long)
	if !strings.Contains(out, "…(전문은 결재 상세에서)") {
		t.Fatal("long analysis should be truncated with a pointer")
	}
	if approvalLogText("제목\n## 주입\t시도") != "제목 ## 주입 시도" {
		t.Fatalf("log text sanitize: %q", approvalLogText("제목\n## 주입\t시도"))
	}
	stripped := approvalAnalysisExcerpt("요지\nIMPORTANCE: routine\nPROJECT_FILE: yes\n핵심")
	if strings.Contains(stripped, "IMPORTANCE:") || strings.Contains(stripped, "PROJECT_FILE:") {
		t.Fatalf("trailers should strip: %q", stripped)
	}
}

func TestApprovalCostStatusLine(t *testing.T) {
	if got := approvalCostStatusLine("t", "한화", "1.2억", nil); got != "결재 비용 한화 1.2억" {
		t.Fatalf("amount line: %q", got)
	}
	if got := approvalCostStatusLine("t", "한화", "1.2억", []string{"품목 모듈: 단가 145 → 152 (+4.8%)"}); !strings.HasPrefix(got, "품목 모듈") {
		t.Fatalf("delta preferred: %q", got)
	}
}
