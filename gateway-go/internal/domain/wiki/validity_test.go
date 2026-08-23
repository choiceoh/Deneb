package wiki

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestValidityFactor_ScalesWithAgeArchivedAndSupersededStatus(t *testing.T) {
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
		{"superseded", Frontmatter{SupersededBy: "거래/new.md", Updated: "2026-06-01"}, 0.15, 0.15},
		{"superseded-and-old", Frontmatter{SupersededBy: "x.md", Updated: "2024-01-01"}, 0.105, 0.104},
	}
	for _, c := range cases {
		got := validityFactor("", c.meta, now)
		if got > c.max+1e-9 || got < c.min-1e-9 {
			t.Errorf("%s: factor=%v want [%v,%v]", c.name, got, c.min, c.max)
		}
	}
}

// TestSearch_DemotesSupersededPageBelowReplacementAndPersistsAcrossRestart: a page
// whose facts were replaced must rank below the page that replaced it, even
// with near-identical text — the exact "year-old port number presented as
// current" failure recall had.
func TestSearch_DemotesSupersededPageBelowReplacementAndPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	old := &Page{
		Meta: Frontmatter{
			ID: "port-old", Title: "게이트웨이 포트 정책 (구)", Category: "운영시스템",
			Summary: "게이트웨이 포트는 18789", Importance: 0.8,
		},
		Body: "게이트웨이 포트는 18789를 사용한다.",
	}
	cur := &Page{
		Meta: Frontmatter{
			ID: "port-new", Title: "게이트웨이 포트 정책", Category: "운영시스템",
			Summary: "게이트웨이 포트는 19000으로 변경", Importance: 0.8,
		},
		Body: "게이트웨이 포트는 19000으로 변경되었다.",
	}
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

// TestSearch_ValiditySurvivesRestart: staleness demotion must hold across a
// store reopen. rebuildIndex (the ONLY boot path) used to repopulate the FTS
// but not the validity map, so after every gateway restart archived/superseded
// pages ranked at full BM25 strength — and since archived pages are never
// rewritten, the demotion stayed dead for the whole process lifetime. The
// archived page is lexically FIRST with a heavier term count so the path
// tiebreak cannot mask a missing demotion.
func TestSearch_ValiditySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	wikiDir, diaryDir := filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	archived := &Page{
		Meta: Frontmatter{
			ID: "port-archived", Title: "게이트웨이 포트 정책 구버전", Category: "운영시스템",
			Summary: "게이트웨이 포트 18789 포트 정책", Archived: true,
		},
		Body: "게이트웨이 포트는 18789. 포트 포트 포트 관련 구식 문서.",
	}
	current := &Page{
		Meta: Frontmatter{
			ID: "port-current", Title: "게이트웨이 포트 정책", Category: "운영시스템",
			Summary: "게이트웨이 포트는 19000",
		},
		Body: "게이트웨이 포트는 19000으로 변경되었다.",
	}
	if err := store.WritePage("운영시스템/a-old.md", archived); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("운영시스템/z-new.md", current); err != nil {
		t.Fatal(err)
	}

	assertCurrentFirst := func(t *testing.T, s *Store, phase string) {
		t.Helper()
		results, err := s.Search(context.Background(), "게이트웨이 포트", 5)
		if err != nil || len(results) < 2 {
			t.Fatalf("%s: search failed: %v (%d results)", phase, err, len(results))
		}
		if results[0].Path != "운영시스템/z-new.md" {
			t.Fatalf("%s: archived page outranks current: %+v", phase, results[:2])
		}
	}
	assertCurrentFirst(t, store, "same-process")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertCurrentFirst(t, reopened, "post-restart")
}

func TestValidityFactor_DemotesUnlinkedMailAnalysis(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	meta := Frontmatter{Updated: "2026-08-20"}
	if got := validityFactor("프로젝트/메일분석/orphan@x.com.md", meta, now); got > 0.26 || got < 0.24 {
		t.Errorf("unlinked mail factor=%v want 0.25", got)
	}
	if got := validityFactor("프로젝트/nde-sun-cbl-001/메일분석/orphan@x.com.md", meta, now); got != 1.0 {
		t.Errorf("filed mail factor=%v want 1.0", got)
	}
}

func TestSearch_DemotesUnlinkedMailBelowFiledTwin(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	body := "SunKean 케이블 납기 회신 — 8월 선적 일정 확인."
	orphan := &Page{
		Meta: Frontmatter{ID: "orphan-sunkean", Title: "SunKean 케이블 납기", Category: "프로젝트"},
		Body: body,
	}
	filed := &Page{
		Meta: Frontmatter{ID: "filed-sunkean", Title: "SunKean 케이블 납기", Category: "프로젝트"},
		Body: body,
	}
	if err := store.WritePage("프로젝트/메일분석/a@sunkean.com.md", orphan); err != nil {
		t.Fatal(err)
	}
	if err := store.WritePage("프로젝트/nde-sun-cbl-001/메일분석/a@sunkean.com.md", filed); err != nil {
		t.Fatal(err)
	}

	results, err := store.Search(context.Background(), "SunKean 케이블 납기", 5)
	if err != nil || len(results) < 2 {
		t.Fatalf("search: %v results=%+v", err, results)
	}
	if results[0].Path != "프로젝트/nde-sun-cbl-001/메일분석/a@sunkean.com.md" {
		t.Errorf("unlinked orphan outranked filed twin: %+v", results)
	}
}
