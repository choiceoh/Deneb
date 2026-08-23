package wiki

import (
	"context"
	"strings"
	"testing"
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
