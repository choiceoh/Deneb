package notebook

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestCreateAndGet(t *testing.T) {
	s := newTestStore(t)
	nb, err := s.Create("탑솔라 딜", "2026 공급 계약")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if nb.ID == "" {
		t.Fatal("Create returned empty ID")
	}
	if nb.Name != "탑솔라 딜" || nb.Description != "2026 공급 계약" {
		t.Fatalf("unexpected notebook: %+v", nb)
	}

	got, ok := s.Get(nb.ID)
	if !ok {
		t.Fatal("Get: not found")
	}
	if got.Name != nb.Name {
		t.Fatalf("Get name = %q, want %q", got.Name, nb.Name)
	}
}

func TestCreateRequiresName(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Create("   ", ""); err == nil {
		t.Fatal("Create with blank name should error")
	}
}

func TestUniqueIDOnSameName(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("deal", "")
	b, _ := s.Create("deal", "")
	if a.ID == b.ID {
		t.Fatalf("expected distinct ids, both %q", a.ID)
	}
}

func TestAddSourceCiteMonotonicAcrossRemoval(t *testing.T) {
	s := newTestStore(t)
	nb, _ := s.Create("nb", "")

	s1, err := s.AddSource(nb.ID, Source{Kind: KindNote, Text: "first"})
	if err != nil {
		t.Fatalf("AddSource s1: %v", err)
	}
	s2, _ := s.AddSource(nb.ID, Source{Kind: KindNote, Text: "second"})
	if s1.Cite != "S1" || s2.Cite != "S2" {
		t.Fatalf("cites = %q,%q want S1,S2", s1.Cite, s2.Cite)
	}

	// Remove S1; the next add must NOT reuse S1 or collide with S2.
	if err := s.RemoveSource(nb.ID, "S1"); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	s3, _ := s.AddSource(nb.ID, Source{Kind: KindNote, Text: "third"})
	if s3.Cite != "S3" {
		t.Fatalf("cite after removal = %q, want S3 (no reuse/collision)", s3.Cite)
	}

	got, _ := s.Get(nb.ID)
	if len(got.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(got.Sources))
	}
}

func TestAddSourceValidation(t *testing.T) {
	s := newTestStore(t)
	nb, _ := s.Create("nb", "")

	cases := []struct {
		name string
		src  Source
		ok   bool
	}{
		{"wiki ok", Source{Kind: KindWiki, Ref: "프로젝트/x.md"}, true},
		{"wiki no ref", Source{Kind: KindWiki}, false},
		{"note ok", Source{Kind: KindNote, Text: "hi"}, true},
		{"note no text", Source{Kind: KindNote}, false},
		{"bad kind", Source{Kind: "url", Ref: "http://x"}, false},
	}
	for _, tc := range cases {
		_, err := s.AddSource(nb.ID, tc.src)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("%s: expected error, got nil", tc.name)
		}
	}
}

func TestAddSourceUnknownNotebook(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddSource("nope", Source{Kind: KindNote, Text: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRemoveUnknownSource(t *testing.T) {
	s := newTestStore(t)
	nb, _ := s.Create("nb", "")
	if err := s.RemoveSource(nb.ID, "S9"); err == nil {
		t.Fatal("removing unknown cite should error")
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	nb, _ := s.Create("nb", "")
	if err := s.Delete(nb.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get(nb.ID); ok {
		t.Fatal("notebook still present after Delete")
	}
	if err := s.Delete(nb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete err = %v, want ErrNotFound", err)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	nb, _ := s.Create("탑솔라", "딜")
	if _, err := s.AddSource(nb.ID, Source{Kind: KindNote, Text: "pinned"}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}

	// Reopen from disk — the notebook and its source must survive.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen NewStore: %v", err)
	}
	got, ok := s2.Get(nb.ID)
	if !ok {
		t.Fatal("notebook not loaded after reopen")
	}
	if len(got.Sources) != 1 || got.Sources[0].Text != "pinned" {
		t.Fatalf("source not persisted: %+v", got.Sources)
	}

	// A JSON file should exist on disk for the notebook.
	if _, err := os.Stat(filepath.Join(dir, nb.ID+".json")); err != nil {
		t.Fatalf("notebook file missing: %v", err)
	}
}

func TestListSortedByUpdated(t *testing.T) {
	s := newTestStore(t)
	a, _ := s.Create("a", "")
	b, _ := s.Create("b", "")
	// Touch a so it becomes the most-recently-updated.
	if _, err := s.AddSource(a.ID, Source{Kind: KindNote, Text: "x"}); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	list := s.List()
	if len(list) != 2 {
		t.Fatalf("List len = %d, want 2", len(list))
	}
	if list[0].ID != a.ID {
		t.Fatalf("List[0] = %q, want most-recently-updated %q (b=%q)", list[0].ID, a.ID, b.ID)
	}
}

func TestGetReturnsCopy(t *testing.T) {
	s := newTestStore(t)
	nb, _ := s.Create("nb", "")
	_, _ = s.AddSource(nb.ID, Source{Kind: KindNote, Text: "orig"})

	got, _ := s.Get(nb.ID)
	got.Sources[0].Text = "mutated"
	got.Name = "mutated"

	fresh, _ := s.Get(nb.ID)
	if fresh.Sources[0].Text != "orig" || fresh.Name != "nb" {
		t.Fatal("Get did not return an isolated copy; store state was mutated")
	}
}

func TestEnsureForDealIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	a, err := s.EnsureForDeal("프로젝트/탑솔라.md", "탑솔라 딜", "")
	if err != nil {
		t.Fatalf("EnsureForDeal: %v", err)
	}
	if a.DealRef != "프로젝트/탑솔라.md" {
		t.Fatalf("DealRef = %q, want anchored", a.DealRef)
	}
	// Same deal ref → same notebook (get-or-create), not a duplicate.
	b, _ := s.EnsureForDeal("프로젝트/탑솔라.md", "다른 이름", "")
	if b.ID != a.ID {
		t.Fatalf("EnsureForDeal created a duplicate: %q vs %q", b.ID, a.ID)
	}
	if got := s.List(); len(got) != 1 {
		t.Fatalf("notebooks = %d, want 1 (no duplicate)", len(got))
	}
	// Different deal ref → different notebook.
	c, _ := s.EnsureForDeal("프로젝트/other.md", "", "")
	if c.ID == a.ID {
		t.Fatal("distinct deals must get distinct notebooks")
	}
	if c.Name != "프로젝트/other.md" {
		t.Fatalf("blank name should default to deal ref, got %q", c.Name)
	}
}

func TestEnsureForDealRequiresRef(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.EnsureForDeal("  ", "x", ""); err == nil {
		t.Fatal("EnsureForDeal with blank deal ref should error")
	}
}

func TestGetByDealRef(t *testing.T) {
	s := newTestStore(t)
	if _, ok := s.GetByDealRef("프로젝트/탑솔라.md"); ok {
		t.Fatal("GetByDealRef should miss before creation")
	}
	created, _ := s.EnsureForDeal("프로젝트/탑솔라.md", "탑솔라", "")
	got, ok := s.GetByDealRef("프로젝트/탑솔라.md")
	if !ok || got.ID != created.ID {
		t.Fatalf("GetByDealRef = %+v ok=%v, want %q", got, ok, created.ID)
	}
}

func TestDealRefPersists(t *testing.T) {
	dir := t.TempDir()
	s, _ := NewStore(dir)
	nb, _ := s.EnsureForDeal("프로젝트/탑솔라.md", "탑솔라", "")
	_, _ = s.AddSource(nb.ID, Source{Kind: KindNote, Text: "견적"})

	s2, _ := NewStore(dir)
	got, ok := s2.GetByDealRef("프로젝트/탑솔라.md")
	if !ok {
		t.Fatal("deal-anchored notebook not reloaded")
	}
	if got.DealRef != "프로젝트/탑솔라.md" || len(got.Sources) != 1 {
		t.Fatalf("deal anchor/source not persisted: %+v", got)
	}
}

func TestPinUniqueDedupsByRefAndCreatesDeal(t *testing.T) {
	s := newTestStore(t)
	const deal = "프로젝트/탑솔라.md"

	// First pin creates the deal notebook and adds the source.
	added, err := s.PinUnique(deal, "탑솔라", Source{Kind: KindNote, Ref: "mail:abc", Text: "견적"})
	if err != nil || !added {
		t.Fatalf("first PinUnique added=%v err=%v, want added", added, err)
	}
	nb, ok := s.GetByDealRef(deal)
	if !ok || len(nb.Sources) != 1 {
		t.Fatalf("deal notebook not created/pinned: %+v ok=%v", nb, ok)
	}

	// Re-pinning the same Ref (same mail re-analyzed) is an idempotent no-op.
	added, err = s.PinUnique(deal, "탑솔라", Source{Kind: KindNote, Ref: "mail:abc", Text: "견적(재분석)"})
	if err != nil || added {
		t.Fatalf("duplicate PinUnique added=%v err=%v, want not-added", added, err)
	}
	if nb, _ := s.GetByDealRef(deal); len(nb.Sources) != 1 {
		t.Fatalf("dedup failed: sources = %d, want 1", len(nb.Sources))
	}

	// A different Ref on the same deal does add (and reuses the notebook).
	added, _ = s.PinUnique(deal, "탑솔라", Source{Kind: KindNote, Ref: "mail:def", Text: "계약서"})
	if !added {
		t.Fatal("distinct ref should be added")
	}
	if got := s.List(); len(got) != 1 {
		t.Fatalf("PinUnique created a duplicate notebook: %d", len(got))
	}
	if nb, _ := s.GetByDealRef(deal); len(nb.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(nb.Sources))
	}
}

func TestPinUniqueRequiresRefAndValidSource(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.PinUnique("", "x", Source{Kind: KindNote, Text: "y"}); err == nil {
		t.Fatal("PinUnique without deal ref should error")
	}
	if _, err := s.PinUnique("deal", "x", Source{Kind: KindNote}); err == nil {
		t.Fatal("PinUnique with invalid source (no text) should error")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"탑솔라 딜":          "탑솔라-딜",
		"  Deal / 2026 ": "deal-2026",
		"!!!":            "notebook",
		"a__b":           "a-b",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStampProjectRefs(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	dealRef := "프로젝트/거래/한빛전기.md" // counterparty-keyed; differs from the project name
	if _, err := s.EnsureForDeal(dealRef, "한빛전기 딜", ""); err != nil {
		t.Fatalf("EnsureForDeal: %v", err)
	}
	before, _ := s.GetByDealRef(dealRef)

	// First stamp records the resolved project page.
	added, err := s.StampProjectRefs(dealRef, []string{"프로젝트/영산고.md"})
	if err != nil || !added {
		t.Fatalf("StampProjectRefs first = (%v, %v), want (true, nil)", added, err)
	}
	got, _ := s.GetByDealRef(dealRef)
	if len(got.ProjectRefs) != 1 || got.ProjectRefs[0] != "프로젝트/영산고.md" {
		t.Fatalf("ProjectRefs = %v, want [프로젝트/영산고.md]", got.ProjectRefs)
	}
	// Stamping is metadata enrichment, not activity — Updated must stay stable so a
	// re-analysis doesn't churn the list order.
	if got.Updated != before.Updated {
		t.Fatalf("Updated changed on stamp: %d → %d", before.Updated, got.Updated)
	}

	// Re-stamping the same ref is an idempotent no-op.
	added, err = s.StampProjectRefs(dealRef, []string{"프로젝트/영산고.md"})
	if err != nil || added {
		t.Fatalf("StampProjectRefs dup = (%v, %v), want (false, nil)", added, err)
	}
	got, _ = s.GetByDealRef(dealRef)
	if len(got.ProjectRefs) != 1 {
		t.Fatalf("dup stamp grew ProjectRefs to %v", got.ProjectRefs)
	}

	// A new ref unions in; an already-present one in the same call is skipped.
	added, err = s.StampProjectRefs(dealRef, []string{"프로젝트/영산고.md", "프로젝트/제2발전소.md", "  "})
	if err != nil || !added {
		t.Fatalf("StampProjectRefs union = (%v, %v), want (true, nil)", added, err)
	}
	got, _ = s.GetByDealRef(dealRef)
	if len(got.ProjectRefs) != 2 {
		t.Fatalf("ProjectRefs = %v, want 2 unique", got.ProjectRefs)
	}

	// Stamping a dealRef with no notebook is a no-op (the pin creates it first).
	added, err = s.StampProjectRefs("프로젝트/거래/없는딜.md", []string{"프로젝트/영산고.md"})
	if err != nil || added {
		t.Fatalf("StampProjectRefs unknown = (%v, %v), want (false, nil)", added, err)
	}
	// Empty inputs are no-ops, not errors.
	if added, err := s.StampProjectRefs("", []string{"x"}); err != nil || added {
		t.Fatalf("empty dealRef = (%v, %v), want (false, nil)", added, err)
	}
	if added, err := s.StampProjectRefs(dealRef, nil); err != nil || added {
		t.Fatalf("nil refs = (%v, %v), want (false, nil)", added, err)
	}

	// Refs survive a reload from disk.
	s2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	reloaded, ok := s2.GetByDealRef(dealRef)
	if !ok || len(reloaded.ProjectRefs) != 2 {
		t.Fatalf("after reload ProjectRefs = %v (ok=%v), want 2", reloaded.ProjectRefs, ok)
	}
}
