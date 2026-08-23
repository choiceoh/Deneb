package wiki

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSelectSynthesisIndexEntries_KeepsAnchorsHighImportanceAndRecent(t *testing.T) {
	now := time.Now()
	old := now.AddDate(0, 0, -60).Format("2006-01-02")
	recent := now.AddDate(0, 0, -2).Format("2006-01-02")

	entries := map[string]IndexEntry{
		"프로젝트/anchor/대표.md": {Category: "프로젝트", Importance: 0.1, Updated: old},
		"시스템/core.md":       {Category: "시스템", Importance: 0.9, Updated: old},
		"업무/recent.md":      {Category: "업무", Importance: 0.1, Updated: recent},
	}
	// A large low-importance, old tail that the cap should push out.
	for i := 0; i < 300; i++ {
		entries[fmt.Sprintf("프로젝트/p%03d/대표.md", i)] = IndexEntry{Category: "프로젝트", Importance: 0.05, Updated: old}
	}

	got := selectSynthesisIndexEntries(entries, map[string]struct{}{"프로젝트/anchor/대표.md": {}}, now)

	if len(got) != synthesisIndexEntryLimit {
		t.Fatalf("subset size = %d, want cap %d", len(got), synthesisIndexEntryLimit)
	}
	for _, path := range []string{"프로젝트/anchor/대표.md", "시스템/core.md", "업무/recent.md"} {
		if _, ok := got[path]; !ok {
			t.Errorf("routing-relevant page %q must survive the cap", path)
		}
	}
}

func TestSelectSynthesisIndexEntries_NoCapWhenSmall(t *testing.T) {
	entries := map[string]IndexEntry{
		"기타/a.md": {Category: "기타"},
		"기타/b.md": {Category: "기타"},
	}
	got := selectSynthesisIndexEntries(entries, nil, time.Now())
	if len(got) != 2 {
		t.Fatalf("small index must be kept whole, got %d", len(got))
	}
}

func TestRenderSynthesisSubset_RendersOnlySubset(t *testing.T) {
	idx := newIndex()
	idx.Entries["기타/a.md"] = IndexEntry{Category: "기타", Title: "A", Importance: 0.5}
	idx.Entries["기타/b.md"] = IndexEntry{Category: "기타", Title: "B", Importance: 0.5}

	out := idx.RenderSynthesisSubset(map[string]IndexEntry{"기타/a.md": idx.Entries["기타/a.md"]})
	if !strings.Contains(out, "기타/a.md") {
		t.Errorf("subset render must include the selected page: %q", out)
	}
	if strings.Contains(out, "기타/b.md") {
		t.Errorf("subset render must exclude the unselected page: %q", out)
	}
}

func TestCurrentOrdinaryIndexEntries_ExcludesDerivedAndHistoricalPages(t *testing.T) {
	store := missStore(t)
	write := func(path string, page *Page) {
		t.Helper()
		if err := store.WritePage(path, page); err != nil {
			t.Fatal(err)
		}
	}
	write("업무/live.md", &Page{Meta: Frontmatter{Title: "현행"}, Body: "live"})
	write("업무/archived.md", &Page{Meta: Frontmatter{Title: "보관", Archived: true}, Body: "archived"})
	write("업무/old.md", &Page{Meta: Frontmatter{Title: "구버전", SupersededBy: "업무/new.md"}, Body: "old"})
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "간결하게",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}

	entries := store.SnapshotIndex().Entries
	if _, ok := entries[factProfilePagePath]; !ok {
		t.Fatal("test setup: generated fact projection missing from master index")
	}
	got := (&WikiDreamer{store: store}).currentOrdinaryIndexEntries(entries)
	if _, ok := got["업무/live.md"]; !ok {
		t.Fatalf("live page missing from ordinary current subset: %+v", got)
	}
	for _, path := range []string{"업무/archived.md", "업무/old.md", factProfilePagePath} {
		if _, ok := got[path]; ok {
			t.Errorf("non-current page %q entered dreamer index subset", path)
		}
	}
}
