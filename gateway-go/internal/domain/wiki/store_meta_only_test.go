package wiki

import "testing"

// TestUpdatePageMetaOnly_PreservesUpdatedAndRefusesBodyEdits: `updated` means
// "when this knowledge last changed" to the stale-deadline, dormancy, freshness
// and search-age paths. A repair sweep that only rewrites frontmatter must not
// re-date the page (a 2026-08-23 curation sweep bumped 53 인물 pages while only
// trimming `related`, taking "updated ≤7d but body <300B" from 9 to 44).
func TestUpdatePageMetaOnly_PreservesUpdatedAndRefusesBodyEdits(t *testing.T) {
	s, _ := newVerifyStore(t)
	rel := "인물/강동민.md"
	writePageT(t, s, rel, "강동민", "인물", "본문")
	if err := s.UpdatePage(rel, func(cur *Page) (*Page, error) {
		cur.Meta.Updated = "2026-07-28"
		cur.Meta.Related = []string{"인물/강민수.md", "인물/강은구.md"}
		return cur, nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := s.UpdatePageMetaOnly(rel, func(cur *Page) (*Page, error) {
		cur.Meta.Related = cur.Meta.Related[:1]
		cur.Meta.Updated = "2026-08-23" // a careless mutate must not win
		return cur, nil
	}); err != nil {
		t.Fatalf("meta-only update: %v", err)
	}
	got, _ := s.ReadPage(rel)
	if got.Meta.Updated != "2026-07-28" {
		t.Errorf("updated bumped by a metadata-only repair: %q", got.Meta.Updated)
	}
	if len(got.Meta.Related) != 1 {
		t.Errorf("metadata change not applied: %+v", got.Meta.Related)
	}

	// A body edit through this door is a programming error, not a silent
	// re-dating: refuse it so the caller uses UpdatePage.
	err := s.UpdatePageMetaOnly(rel, func(cur *Page) (*Page, error) {
		cur.Body += "\n추가"
		return cur, nil
	})
	if err == nil {
		t.Error("body edit through UpdatePageMetaOnly was accepted")
	}
	if after, _ := s.ReadPage(rel); after.Body != "본문" {
		t.Errorf("refused body edit still landed: %q", after.Body)
	}
}
