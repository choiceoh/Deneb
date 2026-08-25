package wiki

import (
	"context"
	"strings"
	"testing"
	"time"
)

func writePersonPageT(t *testing.T, s *Store, rel, title string, emails []string, body string) {
	t.Helper()
	p := NewPage(title, "인물", nil)
	p.Meta.Emails = emails
	p.Body = body
	if err := s.WritePage(rel, p); err != nil {
		t.Fatalf("WritePage %q: %v", rel, err)
	}
}

// The scan flags a person whose mail-analysis From is neither listed on the
// page nor on a domain the page already owns — the same rule the recall lane's
// ⚠ marker applies, kept in one place so the two surfaces cannot drift.
func TestPersonMailConflicts(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	writePersonPageT(t, s, "인물/홍길동.md", "홍길동",
		[]string{"hong@topsolar.kr"}, "담당: 태안 프로젝트")
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/격적-요청.md", "격적 요청", "프로젝트",
		"**발신** 홍길동 <hong@newco.com>\n\n홍길동 님이 견적 스펙 변경을 요청했습니다.")
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/정기-보고.md", "정기 보고", "프로젝트",
		"From: 홍길동 <gil@topsolar.kr>\n\n홍길동 님 정기 보고입니다.")

	got := s.PersonMailConflicts(ctx, 10)
	if len(got) != 1 {
		t.Fatalf("conflicts = %+v, want exactly the newco mismatch", got)
	}
	if got[0].PagePath != "인물/홍길동.md" || got[0].MailFrom != "hong@newco.com" {
		t.Fatalf("conflict = %+v", got[0])
	}
	if !strings.Contains(got[0].MailPath, "메일분석/") {
		t.Fatalf("mail path = %q, want a mail-analysis page", got[0].MailPath)
	}
	// The same-domain From (gil@topsolar.kr) must NOT conflict — the page owns
	// the topsolar.kr domain.
}

func TestPersonMailConflictsKeepsFactResultsOutOfItsPageWindow(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	writePersonPageT(t, s, "인물/홍길동.md", "홍길동",
		[]string{"hong@topsolar.kr"}, strings.Repeat("홍길동 ", 20)+"담당: 태안 프로젝트")
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/주소-변경.md", "주소 변경", "프로젝트",
		"**발신** 홍길동 <hong@newco.com>\n\n홍길동 님이 새 주소로 회신했습니다.")
	base := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	for i, key := range []string{"profile.name", "profile.alias", "profile.role"} {
		if _, err := s.UpsertFact(FactInput{
			Subject: "person:홍길동", Key: key, Value: "홍길동",
			Kind: FactKindIdentity, Authority: FactAuthorityAgent,
			At: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("UpsertFact(%q): %v", key, err)
		}
	}

	crowded, err := s.Search(ctx, "홍길동", personConflictHitsMax)
	if err != nil {
		t.Fatal(err)
	}
	factHits, mailHits := 0, 0
	for _, hit := range crowded {
		if hit.FactID != "" {
			factHits++
		}
		if IsMailAnalysisPath(hit.Path) {
			mailHits++
		}
	}
	if factHits == 0 || mailHits != 0 {
		t.Fatalf("test setup did not crowd the page window: %+v", crowded)
	}

	got := s.PersonMailConflicts(ctx, 10)
	if len(got) != 1 || got[0].MailFrom != "hong@newco.com" {
		t.Fatalf("conflicts = %+v, want the mail mismatch despite fact hits", got)
	}
}

func TestPersonMailConflictNegativeCases(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	// Listed email exactly matches the From → no conflict.
	writePersonPageT(t, s, "인물/이서연.md", "이서연",
		[]string{"seoyeon@partner.com"}, "")
	writePageT(t, s, "프로젝트/pl9-tst-002/메일분석/인사.md", "인사", "프로젝트",
		"발신: 이서연 <seoyeon@partner.com>\n\n이서연 인사 메일입니다.")
	// No emails on the page → never scanned.
	writePersonPageT(t, s, "인물/무연락.md", "무연락", nil, "산문 없음")
	writePageT(t, s, "프로젝트/pl9-tst-002/메일분석/무연락-메일.md", "무연락 메일", "프로젝트",
		"발신: 무연락 <x@y.z>\n\n무연락 관련")

	if got := s.PersonMailConflicts(ctx, 10); len(got) != 0 {
		t.Fatalf("conflicts = %+v, want none", got)
	}
}

// The scan used to read the SEARCH SNIPPET, so "the first address in it" was
// routinely a recipient or a quoted address — and a body mention counted as
// "about this person". Live 2026-08-25: 강동민 was filed against a 태한
// RECIPIENT, 김건호 against a mail 박종원 sent.
func TestPersonMailConflicts_IgnoresRecipientsAndBodyMentions(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	writePersonPageT(t, s, "인물/강동민.md", "강동민", []string{"kangdm@topsolar.kr"}, "탑솔라 소속")
	// A mail 고건 sent, whose RECIPIENT is an outside address and whose body
	// merely mentions 강동민.
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/보증서-송부.md", "보증서 송부", "프로젝트",
		"> From: \"고건\" <taygun152@topsolar.kr>\n\n| **수신** | gocharge89@taihan.com — 태한 담당자 |\n\n강동민 대리가 물량을 산출했습니다.")

	if got := s.PersonMailConflicts(ctx, 10); len(got) != 0 {
		t.Errorf("수신자·본문 언급으로 불일치가 잡힘: %+v", got)
	}
}

// An archived person page must not produce operator cards.
func TestPersonMailConflicts_SkipsArchivedPages(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	page := NewPage("김건호", "인물", nil)
	page.Meta.Emails = []string{"gunbbang70@topsolar.kr"}
	page.Meta.Archived = true
	page.Body = "탑솔라 소속"
	if err := s.WritePage("인물/김건호.md", page); err != nil {
		t.Fatal(err)
	}
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/무림-검토.md", "무림 검토", "프로젝트",
		"> From: 김건호 <tiger0927@moorim.co.kr>\n\n김건호 검토 의견입니다.")

	if got := s.PersonMailConflicts(ctx, 10); len(got) != 0 {
		t.Errorf("보관된 페이지로 카드가 만들어짐: %+v", got)
	}
}

// A mail this person really sent from a new address is still a conflict — the
// precision fix must not cost the signal.
func TestPersonMailConflicts_KeepsRealSenderMismatch(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	writePersonPageT(t, s, "인물/신정훈.md", "신정훈", []string{"jhshin@findgreen.net"}, "파인드그린 대표")
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/추가자료.md", "추가자료", "프로젝트",
		"> From: 신정훈(J.H.SHIN) <cadwin2@naver.com>\n\n신정훈 대표의 회신입니다.")

	got := s.PersonMailConflicts(ctx, 10)
	if len(got) != 1 || got[0].MailFrom != "cadwin2@naver.com" {
		t.Errorf("실제 발신 불일치를 놓침: %+v", got)
	}
}

// A homonym sender recurred every 30 days forever: the lane's only stop was a
// snooze. An operator answer must end it for good, and only for that address.
func TestPersonMailConflicts_HonorsOperatorAnswerPerAddress(t *testing.T) {
	s, _ := newVerifyStore(t)
	ctx := context.Background()

	writePersonPageT(t, s, "인물/박준영.md", "박준영", []string{"jypark@adfamc.com"}, "에이디에프자산운용")
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/입사지원.md", "입사지원", "프로젝트",
		"> From: 박준영 <persie7@naver.com>\n\n박준영 지원자의 입사지원서입니다.")

	if got := s.PersonMailConflicts(ctx, 10); len(got) != 1 {
		t.Fatalf("불일치가 안 잡힘: %+v", got)
	}
	page, err := s.ReadPage("인물/박준영.md")
	if err != nil {
		t.Fatal(err)
	}
	page.Meta.IdentityReviewed = []string{MailConflictAckToken("persie7@naver.com")}
	if err := s.WritePage("인물/박준영.md", page); err != nil {
		t.Fatal(err)
	}
	if got := s.PersonMailConflicts(ctx, 10); len(got) != 0 {
		t.Errorf("확인했는데 계속 물어봄: %+v", got)
	}

	// The answer covers that address only — a different unknown sender is new.
	writePageT(t, s, "프로젝트/pl9-tst-001/메일분석/다른건.md", "다른건", "프로젝트",
		"> From: 박준영 <other@somewhere.co.kr>\n\n박준영 님 회신입니다.")
	if got := s.PersonMailConflicts(ctx, 10); len(got) != 1 {
		t.Errorf("새 주소가 조용함: %+v", got)
	}
}
