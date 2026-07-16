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
		DocID:      "99172",
		Title:      "완도 관산포 프로젝트 관련 금전대여의 건",
		Drafter:    "공명한",
		Importance: "attention",
		Analysis:   "요지: 금전대여 승인 요청\nIMPORTANCE: attention\n핵심: 대여금 3억",
	}
	s.logApprovalAnalysisToWiki(rec)

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
	if strings.Contains(page.Body, "IMPORTANCE:") {
		t.Fatal("IMPORTANCE marker should be dropped from the gist")
	}

	// Re-log (force rerun) must not duplicate the section.
	s.logApprovalAnalysisToWiki(rec)
	page, _ = ws.ReadPage(wiki.LogPagePath("완도 관산포"))
	if got := strings.Count(page.Body, "ref=approval:99172"); got != 1 {
		t.Fatalf("expected 1 marker, got %d", got)
	}
}

func TestLogApprovalAnalysisToWiki_SkipsWithoutUniqueProject(t *testing.T) {
	s, ws := approvalWikiServer(t)
	s.logApprovalAnalysisToWiki(&groupware.ApprovalAnalysisRecord{
		DocID:    "1",
		Title:    "다과비 추가 구입금액 지출 품의의 건",
		Analysis: "요지: 다과비",
	})
	if page, _ := ws.ReadPage(wiki.LogPagePath("완도 관산포")); page != nil && strings.Contains(page.Body, "다과비") {
		t.Fatal("unrelated approval must not land in the project log")
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
}
