package wiki

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestValidityFactor(t *testing.T) {
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		meta Frontmatter
		max  float64 // factor must be <= max
		min  float64 // and >= min
	}{
		{"fresh", Frontmatter{Updated: "2026-06-01"}, 1.0, 1.0},
		{"old-180d", Frontmatter{Updated: "2025-11-01"}, 0.85, 0.85},
		{"old-365d", Frontmatter{Updated: "2024-01-01"}, 0.7, 0.7},
		{"archived", Frontmatter{Archived: true, Updated: "2026-06-01"}, 0.3, 0.3},
		{"superseded", Frontmatter{SupersededBy: "거래/new.md", Updated: "2026-06-01"}, 0.5, 0.5},
		{"superseded-and-old", Frontmatter{SupersededBy: "x.md", Updated: "2024-01-01"}, 0.35, 0.34},
	}
	for _, c := range cases {
		got := validityFactor(c.meta, now)
		if got > c.max+1e-9 || got < c.min-1e-9 {
			t.Errorf("%s: factor=%v want [%v,%v]", c.name, got, c.min, c.max)
		}
	}
}

// TestSearch_DemotesSupersededPages: a page whose facts were replaced must
// rank below the page that replaced it, even with near-identical text — the
// exact "year-old port number presented as current" failure recall had.
func TestSearch_DemotesSupersededPages(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	old := &Page{Meta: Frontmatter{ID: "port-old", Title: "게이트웨이 포트 정책 (구)", Category: "운영시스템",
		Summary: "게이트웨이 포트는 18789", Importance: 0.8},
		Body: "게이트웨이 포트는 18789를 사용한다."}
	cur := &Page{Meta: Frontmatter{ID: "port-new", Title: "게이트웨이 포트 정책", Category: "운영시스템",
		Summary: "게이트웨이 포트는 19000으로 변경", Importance: 0.8},
		Body: "게이트웨이 포트는 19000으로 변경되었다."}
	if err := store.WritePage("운영시스템/port-old.md", old); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("운영시스템/port-new.md", cur); err != nil {
		t.Fatal(err)
	}

	if err := store.MarkSuperseded("운영시스템/port-old", "운영시스템/port-new.md"); err != nil {
		t.Fatalf("MarkSuperseded: %v", err)
	}
	// Persisted on disk, not just in memory.
	reread, err := store.ReadPage("운영시스템/port-old.md")
	if err != nil || reread.Meta.SupersededBy != "운영시스템/port-new.md" {
		t.Fatalf("superseded_by not persisted: %+v err=%v", reread.Meta, err)
	}

	results, err := store.Search(context.Background(), "게이트웨이 포트", 5)
	if err != nil || len(results) < 2 {
		t.Fatalf("search: %v results=%+v", err, results)
	}
	if results[0].Path != "운영시스템/port-new.md" {
		t.Errorf("superseded page outranks its replacement: %+v", results)
	}

	// Restart: validity must rebuild from disk.
	store2, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	results2, err := store2.Search(context.Background(), "게이트웨이 포트", 5)
	if err != nil || len(results2) < 2 || results2[0].Path != "운영시스템/port-new.md" {
		t.Errorf("validity demotion lost after restart: %+v err=%v", results2, err)
	}
}
