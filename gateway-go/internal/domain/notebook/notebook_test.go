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
