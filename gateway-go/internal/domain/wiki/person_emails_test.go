package wiki

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

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

	book := []Contact{
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
