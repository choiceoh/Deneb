package wiki

import (
	"testing"
	"time"
)

func TestActiveCounterpartyDomainsExpiresStale(t *testing.T) {
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

	// Reclassify churn: an old unlinked analysis moved into a project gets its
	// Updated re-stamped to today, but the window keys off the immutable
	// Created — the stale domain must NOT re-activate for another 60 days.
	if err := store.WritePage("프로젝트/영산고/메일분석/m5.md", &Page{
		Meta: Frontmatter{
			Title: "m5", Category: "프로젝트", Type: "log", Confidence: "medium",
			Tags: []string{"resurrected.kr"}, Created: "2020-01-01", Updated: today,
		},
		Body: "분석",
	}); err != nil {
		t.Fatal(err)
	}

	got := store.ActiveCounterpartyDomains("2026-01-01")
	if _, ok := got["acme.co.kr"]; !ok {
		t.Errorf("acme.co.kr missing from active set: %v", got)
	}
	for _, excluded := range []string{"gmail.com", "old-corp.kr", "bucket-only.kr", "resurrected.kr"} {
		if _, ok := got[excluded]; ok {
			t.Errorf("%s must be excluded, set: %v", excluded, got)
		}
	}
}

// CounterpartyProjects must mirror the domain-set rules (window, freemail,
// linked-only) and add per-domain project lists ordered by latest activity,
// capped and deterministically tie-broken.
func TestCounterpartyProjectsReturnsOrderedAndCapped(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, domainTag, created string) {
		t.Helper()
		if err := store.WritePage(path, &Page{
			Meta: Frontmatter{
				Title: path, Category: "프로젝트", Type: "log", Confidence: "medium",
				Tags: []string{domainTag}, Created: created, Updated: created,
			},
			Body: "분석",
		}); err != nil {
			t.Fatal(err)
		}
	}

	// hre.kr spans two projects — 당진 more recent than 영덕.
	write("프로젝트/당진-솔라빌리지/메일분석/a1.md", "hre.kr", "2026-07-03")
	write("프로젝트/영덕-풍력/메일분석/a2.md", "hre.kr", "2026-06-20")
	write("프로젝트/당진-솔라빌리지/메일분석/a3.md", "gmail.com", "2026-07-03") // freemail — excluded
	write("프로젝트/메일분석/a4.md", "bucket.kr", "2026-07-03")          // unlinked — excluded
	write("프로젝트/옛거래/메일분석/a5.md", "hre.kr", "2020-01-01")         // stale — excluded

	got := store.CounterpartyProjects("2026-06-01")
	projs := got["hre.kr"]
	if len(projs) != 2 || projs[0] != "당진-솔라빌리지" || projs[1] != "영덕-풍력" {
		t.Fatalf("hre.kr projects = %v, want [당진-솔라빌리지 영덕-풍력]", projs)
	}
	if _, ok := got["gmail.com"]; ok {
		t.Fatal("freemail domain must not appear")
	}
	if _, ok := got["bucket.kr"]; ok {
		t.Fatal("unlinked bucket mail must not appear")
	}
	if projs := got["hre.kr"]; len(projs) > maxCounterpartyProjects {
		t.Fatalf("project list must be capped at %d", maxCounterpartyProjects)
	}
}
