package wiki

import (
	"context"
	"path/filepath"
	"strings"
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
		{"superseded", Frontmatter{SupersededBy: "거래/new.md", Updated: "2026-06-01"}, 0, 0},
		{"superseded-and-old", Frontmatter{SupersededBy: "x.md", Updated: "2024-01-01"}, 0, 0},
	}
	for _, c := range cases {
		got := validityFactor("", &Page{Meta: c.meta}, now)
		if got > c.max+1e-9 || got < c.min-1e-9 {
			t.Errorf("%s: factor=%v want [%v,%v]", c.name, got, c.min, c.max)
		}
	}
}

// TestSearch_ExcludesSupersededPageAndPersistsAcrossRestart: a page whose facts
// were replaced remains directly readable as history but must not appear on a
// current-state search surface — even when its old value is the query.
func TestSearch_ExcludesSupersededPageAndPersistsAcrossRestart(t *testing.T) {
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
	if err != nil || len(results) != 1 {
		t.Fatalf("search: %v results=%+v", err, results)
	}
	if results[0].Path != "운영시스템/port-new.md" {
		t.Errorf("search returned a superseded page: %+v", results)
	}
	oldOnly, err := store.Search(context.Background(), "18789", 5)
	if err != nil || len(oldOnly) != 0 {
		t.Fatalf("old-value search resurfaced superseded page: %+v err=%v", oldOnly, err)
	}

	// Restart: validity must rebuild from disk.
	store2, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store2.Close() })
	results2, err := store2.Search(context.Background(), "게이트웨이 포트", 5)
	if err != nil || len(results2) != 1 || results2[0].Path != "운영시스템/port-new.md" {
		t.Errorf("superseded exclusion lost after restart: %+v err=%v", results2, err)
	}
	if _, err := store2.ReadPage("운영시스템/port-old.md"); err != nil {
		t.Errorf("historical page is no longer directly readable: %v", err)
	}
}

func TestEffectiveSupersessionPreservesBadLayoutFlagsAcrossRestart(t *testing.T) {
	root := t.TempDir()
	wikiDir, diaryDir := filepath.Join(root, "wiki"), filepath.Join(root, "diary")
	store, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	const (
		layoutPath  = "프로젝트/pl2-live-epc-001/로그.md"
		layoutValue = "레이아웃생존값4821"
		stalePath   = "기타/정상대체-구.md"
		staleValue  = "정상대체옛값7312"
	)
	layout := NewPage("살아 있는 프로젝트 로그", "프로젝트", nil)
	layout.Meta.Importance = 1
	layout.Meta.SupersededBy = "프로젝트/pl2-live-epc-001/대표.md"
	layout.Body = layoutValue
	stale := NewPage("정상적인 구버전", "기타", nil)
	stale.Meta.Importance = 1
	stale.Meta.SupersededBy = "기타/정상대체-신.md"
	stale.Body = staleValue
	for path, page := range map[string]*Page{layoutPath: layout, stalePath: stale} {
		if err := store.WritePage(path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", path, err)
		}
	}

	assertCurrentness := func(t *testing.T, s *Store, phase string) {
		t.Helper()
		if IsEffectivelySuperseded(layoutPath, layout.Meta) {
			t.Fatalf("%s: invalid layout flag became effective", phase)
		}
		if !IsEffectivelySuperseded(stalePath, stale.Meta) {
			t.Fatalf("%s: ordinary supersession lost", phase)
		}
		if !semanticPageAdmitted(layoutPath, layout) || semanticPageAdmitted(stalePath, stale) {
			t.Fatalf("%s: semantic admission disagrees with effective supersession", phase)
		}
		got, err := s.Search(context.Background(), layoutValue, 5)
		if err != nil || len(got) != 1 || got[0].Path != layoutPath {
			t.Fatalf("%s: live layout page missing from search: %+v err=%v", phase, got, err)
		}
		if got, err := s.Search(context.Background(), staleValue, 5); err != nil || len(got) != 0 {
			t.Fatalf("%s: ordinary superseded page resurfaced: %+v err=%v", phase, got, err)
		}
		tier1 := s.Tier1Pages(0.5)
		seenLayout, seenStale := false, false
		for _, result := range tier1 {
			seenLayout = seenLayout || result.Path == layoutPath
			seenStale = seenStale || result.Path == stalePath
		}
		if !seenLayout || seenStale {
			t.Fatalf("%s: Tier1 currentness mismatch: %+v", phase, tier1)
		}
	}
	assertCurrentness(t, store, "live")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	assertCurrentness(t, reopened, "reopen")
	staleValues := strings.Join(reopened.RecallFactSnapshot().StaleValues, "\n")
	if strings.Contains(staleValues, layoutValue) || !strings.Contains(staleValues, staleValue) {
		t.Fatalf("reopen stale deny mismatch: %q", staleValues)
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
	if got := validityFactor("프로젝트/메일분석/orphan@x.com.md", &Page{Meta: meta}, now); got > 0.26 || got < 0.24 {
		t.Errorf("unlinked mail factor=%v want 0.25", got)
	}
	if got := validityFactor("프로젝트/nde-sun-cbl-001/메일분석/orphan@x.com.md", &Page{Meta: meta}, now); got != 1.0 {
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
