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
