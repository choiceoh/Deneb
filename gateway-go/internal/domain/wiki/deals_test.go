package wiki

import (
	"strings"
	"testing"
	"time"
)

func newDealStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir, dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestUpsertDealPage_CreatesPage(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	relPath, created, err := s.UpsertDealPage(DealPageInput{
		Counterparty: "탑솔라",
		DocType:      "견적서",
		Amount:       "5,000,000원",
		Date:         "2026-06-08",
		DueDate:      "2026-06-30",
		Items:        []string{"태양광 모듈 100장"},
		Summary:      "6월 모듈 공급 견적",
		SourceRef:    "mail:m1",
	}, now)
	if err != nil {
		t.Fatalf("UpsertDealPage: %v", err)
	}
	if !created {
		t.Error("expected created=true for a new deal page")
	}
	if relPath != "프로젝트/거래/탑솔라.md" {
		t.Errorf("relPath = %q, want 프로젝트/거래/탑솔라.md", relPath)
	}

	page, err := s.ReadPage(relPath)
	if err != nil || page == nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if page.Meta.Category != "프로젝트" {
		t.Errorf("category = %q, want 프로젝트", page.Meta.Category)
	}
	if page.Meta.Due != "2026-06-30" {
		t.Errorf("Due = %q, want 2026-06-30 (payment due surfaced to frontmatter)", page.Meta.Due)
	}
	for _, must := range []string{"## 거래 문서", "견적서", "5,000,000원", "6월 모듈 공급 견적", "태양광 모듈 100장"} {
		if !strings.Contains(page.Body, must) {
			t.Errorf("page body missing %q:\n%s", must, page.Body)
		}
	}
}

func TestUpsertDealPage_AppendsSecondDocument(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	if _, _, err := s.UpsertDealPage(DealPageInput{
		Counterparty: "탑솔라", DocType: "견적서", Summary: "견적", SourceRef: "mail:m1",
	}, now); err != nil {
		t.Fatal(err)
	}
	// A later document for the same counterparty appends — does not overwrite.
	relPath, created, err := s.UpsertDealPage(DealPageInput{
		Counterparty: "탑솔라", DocType: "세금계산서", Summary: "세금계산서 발행", SourceRef: "mail:m2",
	}, now.AddDate(0, 0, 5))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Error("second doc for existing counterparty should not create a new page")
	}
	page, _ := s.ReadPage(relPath)
	if !strings.Contains(page.Body, "견적서") || !strings.Contains(page.Body, "세금계산서") {
		t.Errorf("both documents should be logged, got:\n%s", page.Body)
	}
	if got := strings.Count(page.Body, "## 거래 문서"); got != 1 {
		t.Errorf("expected a single 거래 문서 section, got %d:\n%s", got, page.Body)
	}
}

func TestUpsertDealPage_IdempotentBySourceRef(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	in := DealPageInput{Counterparty: "탑솔라", DocType: "견적서", SourceRef: "mail:m1"}

	if _, _, err := s.UpsertDealPage(in, now); err != nil {
		t.Fatal(err)
	}
	before, _ := s.ReadPage("프로젝트/거래/탑솔라.md")

	// Re-filing the same mail (same SourceRef) must be a no-op.
	if _, created, err := s.UpsertDealPage(in, now.AddDate(0, 0, 3)); err != nil || created {
		t.Errorf("re-file should be no-op: created=%v err=%v", created, err)
	}
	after, _ := s.ReadPage("프로젝트/거래/탑솔라.md")
	if before.Body != after.Body {
		t.Errorf("idempotent re-file changed the body:\nBEFORE:\n%s\nAFTER:\n%s", before.Body, after.Body)
	}
	if before.Meta.Updated != after.Meta.Updated {
		t.Errorf("idempotent re-file bumped Updated: %q → %q", before.Meta.Updated, after.Meta.Updated)
	}
}

func TestUpsertDealPage_RequiresCounterparty(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	if _, _, err := s.UpsertDealPage(DealPageInput{DocType: "견적서"}, now); err == nil {
		t.Error("expected error for empty counterparty")
	}
}

func TestDealSlug(t *testing.T) {
	cases := map[string]string{
		"탑솔라":           "탑솔라",
		"TopSolar Inc.": "topsolar-inc",
		"  남도에코  ":      "남도에코",
		"A & B 상사":      "a-b-상사",
	}
	for in, want := range cases {
		if got := dealSlug(in); got != want {
			t.Errorf("dealSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUpsertDealPage_StampsRelatedProjects(t *testing.T) {
	s := newDealStore(t)
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	// Create: the analyzer's resolved project lands in frontmatter Related (deduped),
	// giving the orphaned counterparty page a deal→project graph edge.
	if _, _, err := s.UpsertDealPage(DealPageInput{
		Counterparty:    "한빛전기",
		DocType:         "견적서",
		SourceRef:       "mail:m1",
		RelatedProjects: []string{"프로젝트/영산고.md", "프로젝트/영산고.md"}, // dup → one
	}, now); err != nil {
		t.Fatalf("UpsertDealPage create: %v", err)
	}
	page, _ := s.ReadPage("프로젝트/거래/한빛전기.md")
	if got := page.Meta.Related; len(got) != 1 || got[0] != "프로젝트/영산고.md" {
		t.Fatalf("Related after create = %v, want [프로젝트/영산고.md]", page.Meta.Related)
	}

	// A later document for the same deal links a second project → unioned, not replaced.
	if _, _, err := s.UpsertDealPage(DealPageInput{
		Counterparty:    "한빛전기",
		DocType:         "계약서",
		SourceRef:       "mail:m2",
		RelatedProjects: []string{"프로젝트/영산고.md", "프로젝트/남도풍력.md"},
	}, now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("UpsertDealPage append: %v", err)
	}
	page, _ = s.ReadPage("프로젝트/거래/한빛전기.md")
	if got := page.Meta.Related; len(got) != 2 || got[0] != "프로젝트/영산고.md" || got[1] != "프로젝트/남도풍력.md" {
		t.Fatalf("Related after union = %v, want [영산고, 남도풍력]", page.Meta.Related)
	}

	// A deal whose mail linked no project gets no Related — and doesn't crash.
	if _, _, err := s.UpsertDealPage(DealPageInput{
		Counterparty: "무명상사",
		DocType:      "견적서",
		SourceRef:    "mail:m3",
	}, now); err != nil {
		t.Fatalf("UpsertDealPage no-related: %v", err)
	}
	page, _ = s.ReadPage("프로젝트/거래/무명상사.md")
	if len(page.Meta.Related) != 0 {
		t.Fatalf("Related = %v, want empty for a project-less deal", page.Meta.Related)
	}
}
