package server

import (
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// writeAutoFlowPage puts a 메일분석 page shaped like the ones the AutoFlow notice
// produces: the title carries the prefix plus the recording name.
func writeAutoFlowPage(t *testing.T, ws *wiki.Store, relPath, title string) {
	t.Helper()
	page := wiki.NewPage(title, "프로젝트", []string{"메일분석"})
	page.Body = "> From: \"PLAUD.AI\" <no-reply@plaud.ai>\n\n회의 요약입니다."
	if err := ws.WritePage(relPath, page); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func TestFoldCoveredMailAnalysisRemovesTheDuplicate(t *testing.T) {
	s, ws := discardServer(t)
	const dupe = "프로젝트/pl1-gsn-dev-001/메일분석/ses-1.md"
	writeAutoFlowPage(t, ws, dupe,
		"[Plaud-AutoFlow] 07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의")

	got, ok := s.foldCoveredMailAnalysis("07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의")
	if !ok || got != dupe {
		t.Fatalf("fold = (%q, %v), want (%q, true)", got, ok, dupe)
	}
	if page, err := ws.ReadPage(dupe); err == nil && page != nil {
		t.Fatal("duplicate page survived the fold")
	}
}

// The mail analyzer and the meeting service disagree about projects, so the fold
// must find the page wherever it was filed.
func TestFoldCoveredMailAnalysisIgnoresProject(t *testing.T) {
	s, ws := discardServer(t)
	const dupe = "프로젝트/com-sds-epc-001/메일분석/ses-2.md"
	writeAutoFlowPage(t, ws, dupe,
		"[Plaud-AutoFlow] 08-24 주간 회의: 태양광·ESS 프로젝트 인허가, 계약, 자재 조달 이슈")

	if got, ok := s.foldCoveredMailAnalysis("08-24 주간 회의: 태양광·ESS 프로젝트 인허가, 계약, 자재 조달 이슈"); !ok || got != dupe {
		t.Fatalf("fold = (%q, %v), want (%q, true)", got, ok, dupe)
	}
}

// Everything that is not this exact meeting's AutoFlow page must survive. A fold
// that reaches one page too far deletes a record nothing else holds.
func TestFoldCoveredMailAnalysisLeavesEverythingElse(t *testing.T) {
	s, ws := discardServer(t)
	otherMeeting := "프로젝트/p/메일분석/ses-3.md"
	ordinaryMail := "프로젝트/p/메일분석/ses-4.md"
	writeAutoFlowPage(t, ws, otherMeeting, "[Plaud-AutoFlow] 08-11 회의: 부력체 사출금형 세팅")
	writeAutoFlowPage(t, ws, ordinaryMail, "07-10 회의: 새만금 태양광 시공 협의") // no prefix — a human's mail

	if got, ok := s.foldCoveredMailAnalysis("07-10 회의: 새만금 태양광 시공 협의"); ok {
		t.Fatalf("folded a non-AutoFlow page: %q", got)
	}
	for _, p := range []string{otherMeeting, ordinaryMail} {
		if page, err := ws.ReadPage(p); err != nil || page == nil {
			t.Fatalf("page %s was removed", p)
		}
	}
}

func TestFoldCoveredMailAnalysisEmptyName(t *testing.T) {
	s, _ := discardServer(t)
	if got, ok := s.foldCoveredMailAnalysis("   "); ok {
		t.Fatalf("empty meeting name folded %q", got)
	}
}

// The census is the net: it must see the duplicate the write-time gate raced
// past, and must not count an ordinary mail page.
func TestCountAutoFlowDuplicates(t *testing.T) {
	s, ws := discardServer(t)
	writeAutoFlowPage(t, ws, "프로젝트/p/메일분석/ses-5.md",
		"[Plaud-AutoFlow] 07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의")
	writeAutoFlowPage(t, ws, "프로젝트/p/메일분석/ses-6.md", "견적서 송부의 건")

	if n := s.countAutoFlowDuplicates(); n != 0 {
		t.Fatalf("no 회의록 page yet, want 0 duplicates, got %d", n)
	}
	meeting := wiki.NewPage("07-10 회의", "프로젝트", []string{"회의록"})
	if err := ws.WritePage(
		"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의-ac3a3dd2.md",
		meeting,
	); err != nil {
		t.Fatal(err)
	}
	if n := s.countAutoFlowDuplicates(); n != 1 {
		t.Fatalf("want 1 duplicate once the meeting exists, got %d", n)
	}
}
