package wiki

import "testing"

// The scans may never auto-fix, so without an operator answer they re-detect
// the same people every cycle — measured on the live wiki: 16 pages flagged,
// the same first five reported forever.
func TestHomonymScan_StopsAskingOnceTheOperatorAnswered(t *testing.T) {
	s, _ := newVerifyStore(t)
	writePageT(t, s, "인물/김성환.md", "김성환", "인물",
		"- 이메일: upshgo@topsolar.kr, shkim@bmenergy.co.kr")

	if got := s.HomonymPersonPages(5); len(got) != 1 {
		t.Fatalf("두 회사 도메인을 가진 페이지가 안 걸림: %+v", got)
	}

	page, err := s.ReadPage("인물/김성환.md")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	page.Meta.IdentityReviewed = s.PersonCompanyDomains("인물/김성환.md")
	if err := s.WritePage("인물/김성환.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if got := s.HomonymPersonPages(5); len(got) != 0 {
		t.Errorf("확인 후에도 계속 물어봄: %+v", got)
	}

	// A decision covers the evidence it was made on, not the person forever:
	// a THIRD employer is new information and must surface once.
	page, _ = s.ReadPage("인물/김성환.md")
	page.Body += "\n- 이메일: sh.kim@posco.com"
	if err := s.WritePage("인물/김성환.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	if got := s.HomonymPersonPages(5); len(got) != 1 {
		t.Errorf("새 도메인이 생겼는데 조용함: %+v", got)
	}
}

func TestDuplicatePersonScan_SettlesOnlyWhenEveryPageIsAnswered(t *testing.T) {
	s, _ := newVerifyStore(t)
	writePageT(t, s, "인물/이영민.md", "이영민", "인물", "- 이메일: a@one.co.kr")
	writePageT(t, s, "인물/이영민-차장.md", "이영민 차장", "인물", "- 이메일: b@two.co.kr")

	groups := s.DuplicatePersonGroups(5)
	if len(groups) != 1 {
		t.Fatalf("같은 이름 두 장이 안 묶임: %+v", groups)
	}

	ack := func(path string, peers ...string) {
		t.Helper()
		page, err := s.ReadPage(path)
		if err != nil {
			t.Fatalf("ReadPage %q: %v", path, err)
		}
		for _, peer := range peers {
			page.Meta.IdentityReviewed = append(page.Meta.IdentityReviewed, "dup:"+peer)
		}
		if err := s.WritePage(path, page); err != nil {
			t.Fatalf("WritePage %q: %v", path, err)
		}
	}

	// Half an answer is still an open question.
	ack("인물/이영민.md", "인물/이영민-차장.md")
	if got := s.DuplicatePersonGroups(5); len(got) != 1 {
		t.Errorf("한 장만 확인했는데 그룹이 사라짐: %+v", got)
	}
	ack("인물/이영민-차장.md", "인물/이영민.md")
	if got := s.DuplicatePersonGroups(5); len(got) != 0 {
		t.Errorf("양쪽 다 확인했는데 계속 물어봄: %+v", got)
	}
}

// Answerable findings must bypass the first-time/repeat fold: under that rule a
// question that never changes is never news, so it was never shown again — and
// never answerable either.
func TestSplitAnswerableFindings_KeepsIdentityQuestionsOutOfTheFold(t *testing.T) {
	rest, answerable := splitAnswerableFindings([]verifyFinding{
		{Type: "homonym", PageA: "인물/김성환.md", AckPages: []string{"인물/김성환.md"}},
		{Type: "stale_deadline", PageA: "업무/발주.md"},
		{Type: "duplicate", PageA: "a.md", AckPages: []string{"a.md"}, Fix: &verifyFix{Kind: "merge"}},
	})
	if len(answerable) != 1 || answerable[0].Type != "homonym" {
		t.Errorf("확인 가능한 발견 분리 실패: %+v", answerable)
	}
	if len(rest) != 2 {
		t.Errorf("나머지 발견이 장부 경로에서 빠짐: %+v", rest)
	}
}
