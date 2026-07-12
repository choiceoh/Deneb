package wiki

import (
	"path/filepath"
	"testing"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestEnrichEmployeePages(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	book := []contactdomain.Contact{
		{Name: "강동민", Emails: []string{"kangdm@topsolar.kr"}, Org: "탑솔라"},     // ours → page
		{Name: "고건", Emails: []string{"go@topsolar.kr"}, Org: "탑솔라"},          // ours
		{Name: "고건 주임", Emails: []string{"go@topsolar.kr"}, Org: "탑솔라"},       // 직급 변형 → same page
		{Name: "조동욱", Emails: []string{"cho@pacificoenergy.co.kr"}, Org: "P"}, // external → no page
		{Name: "#이성기 실장", Emails: []string{"lsk@topsolar.kr"}, Org: "탑솔라"},    // junk row → skipped
	}

	res, err := store.EnrichEmployeePages(book, []string{"topsolar.kr"})
	if err != nil {
		t.Fatalf("EnrichEmployeePages: %v", err)
	}

	people, _ := store.listPeopleByName()
	if _, ok := people["강동민"]; !ok {
		t.Errorf("강동민 (our staff) should have a page")
	}
	if _, ok := people["고건"]; !ok {
		t.Errorf("고건 should have a page")
	}
	if _, ok := people["조동욱"]; ok {
		t.Errorf("조동욱 (external domain) must NOT get a page")
	}
	if _, ok := people["이성기"]; ok {
		t.Errorf("junk '#이성기 실장' row must be skipped")
	}
	// 고건 + 고건 주임 collapse to one page.
	if n := len(res.Created); n != 2 {
		t.Errorf("created %d pages, want 2 (강동민, 고건 — dedup 직급 variant)", n)
	}
}

func TestEnrichDealMentionedPages(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// A 프로젝트 page names an external counterparty (임형철) in its body.
	deal := NewPage("트리나 모듈 계약", "프로젝트", nil)
	deal.Body = "# 트리나 모듈\n\n임형철 부장(트리나솔라)과 645W 모듈 단가 협의. 최종원(LONGi) BC 모듈 병행."
	if err := store.WritePage("프로젝트/트리나-모듈.md", deal); err != nil {
		t.Fatalf("WritePage deal: %v", err)
	}

	book := []contactdomain.Contact{
		{Name: "임형철", Emails: []string{"lim@trinasolar.com"}, Org: "Trina"}, // external + named → page
		{Name: "최종원", Emails: []string{"jc@longigroup.com"}, Org: "LONGi"},  // external + named → page
		{Name: "강동민", Emails: []string{"kangdm@topsolar.kr"}, Org: "탑솔라"},   // our staff → NOT here (EmployeePages' job)
		{Name: "박무명", Emails: []string{"park@somewhere.com"}, Org: "X"},     // external but NOT named → no page
		{Name: "이수", Emails: []string{"lee@short.com"}, Org: "Y"},           // 2-rune name → skipped
	}

	res, err := store.EnrichDealMentionedPages(book, []string{"topsolar.kr"})
	if err != nil {
		t.Fatalf("EnrichDealMentionedPages: %v", err)
	}

	people, _ := store.listPeopleByName()
	if _, ok := people["임형철"]; !ok {
		t.Errorf("임형철 (external, named in deal) should have a page")
	}
	if _, ok := people["최종원"]; !ok {
		t.Errorf("최종원 (external, named in deal) should have a page")
	}
	if _, ok := people["강동민"]; ok {
		t.Errorf("강동민 (our domain) must NOT be created here — that's EnrichEmployeePages")
	}
	if _, ok := people["박무명"]; ok {
		t.Errorf("박무명 (external but not named in any deal) must NOT get a page")
	}
	if len(res.Created) != 2 {
		t.Errorf("created %d, want 2 (임형철, 최종원)", len(res.Created))
	}
}

func TestEnrichPersonEmails(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// 오선택: one identity (single company). 김성훈: 동명이인 (Marsh vs 보해).
	// 김세미: same person across a company variant + a personal-mail address.
	for _, title := range []string{"오선택 전무 (기획조정실장)", "김성훈", "김세미"} {
		p := NewPage(title, "인물", nil)
		slug := title // simple slug for the test
		if err := store.WritePage("인물/"+slug+".md", p); err != nil {
			t.Fatalf("WritePage %s: %v", title, err)
		}
	}

	book := []contactdomain.Contact{
		{Name: "오선택 전무", Emails: []string{"osontaek@topsolar.kr"}, Org: "탑솔라"},
		{Name: "김성훈", Emails: []string{"sunghoon.kim@marsh.com"}, Org: "Marsh"},
		{Name: "김성훈", Emails: []string{"akim@bohae.co.kr"}, Org: "보해"},
		{Name: "김세미", Emails: []string{"semi@topsolar.kr"}, Org: "탑솔라"},
		{Name: "김세미", Emails: []string{"semi.personal@gmail.com"}, Org: ""}, // personal — same person
	}

	res, err := store.EnrichPersonEmails(book)
	if err != nil {
		t.Fatalf("EnrichPersonEmails: %v", err)
	}

	// 오선택 written; 김세미 written (personal-mail domain is not a second employer);
	// 김성훈 flagged ambiguous (two distinct companies), NOT written.
	assertContains := func(list []string, want string) bool {
		for _, s := range list {
			if s == want {
				return true
			}
		}
		return false
	}
	if !assertContains(res.Updated, "오선택 전무 (기획조정실장)") {
		t.Errorf("오선택 not in Updated: %v", res.Updated)
	}
	if !assertContains(res.Updated, "김세미") {
		t.Errorf("김세미 (company + personal mail = one identity) not in Updated: %v", res.Updated)
	}
	if !assertContains(res.Ambiguous, "김성훈") {
		t.Errorf("김성훈 (Marsh vs 보해) not flagged Ambiguous: %v", res.Ambiguous)
	}
	if assertContains(res.Updated, "김성훈") {
		t.Errorf("김성훈 must NOT be written (homonym): %v", res.Updated)
	}

	// The written email must be readable back AND resolvable by email.
	if p := store.ResolvePersonByEmail("osontaek@topsolar.kr"); p != "인물/오선택 전무 (기획조정실장).md" {
		t.Errorf("오선택 email resolves to %q", p)
	}
	kim := testutil.Must(store.ReadPage("인물/김세미.md"))
	if len(kim.Meta.Emails) != 2 {
		t.Errorf("김세미 emails = %v, want both company + personal", kim.Meta.Emails)
	}

	// Idempotent: a second run writes nothing.
	res2, _ := store.EnrichPersonEmails(book)
	if len(res2.Updated) != 0 {
		t.Errorf("second run should be a no-op, wrote %v", res2.Updated)
	}
}

// identityEmails must distinguish 이직 (one person who moved — shared mobile,
// keep both addresses) from 동명이인 (different people — distinct phones, flag).
func TestIdentityEmails_JobChangeVsHomonym(t *testing.T) {
	// 이직: same 010 mobile across two employers → one identity, union both emails.
	move := []contactdomain.Contact{
		{Name: "김이직", Phones: []string{"010-1111-2222"}, Emails: []string{"a@oldco.com"}, Org: "Old"},
		{Name: "김이직", Phones: []string{"010-1111-2222", "02-333-4444"}, Emails: []string{"a@newco.kr"}, Org: "New"},
	}
	emails, amb := identityEmails(move)
	if amb {
		t.Errorf("이직 (shared mobile) wrongly flagged 동명이인")
	}
	if len(emails) != 2 {
		t.Errorf("이직 should union both addresses, got %v", emails)
	}

	// 동명이인: two employers, DIFFERENT phones → ambiguous, no address.
	homonym := []contactdomain.Contact{
		{Name: "김성훈", Phones: []string{"010-2790-3500"}, Emails: []string{"x@marsh.com"}, Org: "Marsh"},
		{Name: "김성훈", Phones: []string{"010-3490-9563"}, Emails: []string{"y@bohae.co.kr"}, Org: "보해"},
	}
	if _, amb := identityEmails(homonym); !amb {
		t.Errorf("동명이인 (distinct phones) should be ambiguous")
	}
}
