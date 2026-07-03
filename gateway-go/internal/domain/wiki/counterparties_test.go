package wiki

import (
	"testing"
	"time"
)

func TestActiveCounterpartyDomains(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	write := func(path, domainTag, updated string) {
		t.Helper()
		if err := store.WritePage(path, &Page{
			Meta: Frontmatter{
				Title: path, Category: "프로젝트", Type: "log", Confidence: "medium",
				Tags: []string{domainTag}, Created: updated, Updated: updated,
			},
			Body: "> From: x <x@" + domainTag + ">\n\n분석",
		}); err != nil {
			t.Fatal(err)
		}
	}

	write("프로젝트/영산고/메일분석/m1.md", "acme.co.kr", today)         // active, project-linked
	write("프로젝트/영산고/메일분석/m2.md", "gmail.com", today)          // freemail — excluded
	write("프로젝트/영산고/메일분석/m3.md", "old-corp.kr", "2020-01-01") // stale
	write("프로젝트/메일분석/m4.md", "bucket-only.kr", today)         // unlinked bucket — excluded

	got := store.ActiveCounterpartyDomains("2026-01-01")
	if _, ok := got["acme.co.kr"]; !ok {
		t.Errorf("acme.co.kr missing from active set: %v", got)
	}
	for _, excluded := range []string{"gmail.com", "old-corp.kr", "bucket-only.kr"} {
		if _, ok := got[excluded]; ok {
			t.Errorf("%s must be excluded, set: %v", excluded, got)
		}
	}
}
