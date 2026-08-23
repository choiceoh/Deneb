package wiki

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitSnapSkip short-circuits when git is unavailable.
func gitSnapSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not found; snapshot tests need git")
	}
}

// The whole-cycle revert undoes a snapshot commit; the selective revert
// restores pre-cycle content for updated pages and deletes cycle-created
// pages; the cycle record maps pages to the commit.
func TestDreamCycleRevertRoundTrip(t *testing.T) {
	gitSnapSkip(t)
	s, wd := newVerifyStore(t)
	ctx := context.Background()

	// Baseline: one existing page.
	writePageT(t, s, "업무/기존.md", "기존", "업무", "원본 내용")
	if hash0 := s.SnapshotGit(ctx, "baseline"); hash0 == "" {
		t.Fatal("baseline snapshot failed")
	}

	// A dream-like cycle: update the existing page + create a new one, then
	// snapshot and record the cycle.
	writePageT(t, s, "업무/기존.md", "기존", "업무", "드리머가 바꾼 내용")
	writePageT(t, s, "업무/신규.md", "신규", "업무", "드리머가 만든 페이지")
	hash := s.SnapshotGit(ctx, "dream cycle")
	if hash == "" {
		t.Fatal("cycle snapshot failed")
	}
	cycle := &dreamCycle{appliedPaths: []string{"업무/기존.md", "업무/신규.md"}, mergedPages: []string{"업무/기존.md"}}
	wd.recordDreamCycleChanges(hash, cycle)

	records := wd.LoadRecentDreamCycles(5)
	if len(records) != 1 || records[0].Commit != hash ||
		len(records[0].Learned) != 2 || records[0].Merged[0] != "업무/기존.md" {
		t.Fatalf("cycle record = %+v", records)
	}

	// Selective revert: only the existing page goes back; the created page
	// survives.
	if err := s.RevertDreamPages(ctx, hash, []string{"업무/기존.md"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.ReadPage("업무/기존.md"); p == nil || strings.TrimSpace(p.Body) != "원본 내용" {
		t.Fatalf("existing page must restore pre-cycle body, got %+v", p)
	}
	if p, _ := s.ReadPage("업무/신규.md"); p == nil {
		t.Fatal("untouched page must survive selective revert")
	}

	// Whole-cycle revert of the ORIGINAL cycle commit still available? The
	// selective revert created a new commit; reverting the original now
	// conflicts on 기존.md — verify revert detects and errors rather than
	// corrupting. Instead, exercise RevertDreamCycle on the selective-revert
	// commit's effect: revert the selective revert restores cycle content.
	head, err := s.git(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevertDreamCycle(ctx, head); err != nil {
		t.Fatalf("revert of %s: %v", head, err)
	}
	if p, _ := s.ReadPage("업무/기존.md"); p == nil || strings.TrimSpace(p.Body) != "드리머가 바꾼 내용" {
		t.Fatalf("cycle content must return after revert-of-revert, got %+v", p)
	}

	// Selective revert also handles cycle-created pages: reverting 신규.md
	// against the ORIGINAL cycle hash (its parent lacks it) deletes it.
	if err := s.RevertDreamPages(ctx, hash, []string{"업무/신규.md"}); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.ReadPage("업무/신규.md"); p != nil {
		t.Fatal("cycle-created page must be deleted by selective revert")
	}
}

// The commit lookup resolves a (hash → record) from the ledger the feed's
// per-page revert actions address, and the page list keeps the canonical order
// those actions' indexes were minted in — learned, merged, expired, moved,
// deduped.
func TestDreamCycleChangesByCommitAndPageList(t *testing.T) {
	s, wd := newVerifyStore(t)
	cycle := &dreamCycle{
		appliedPaths: []string{"프로젝트/a/대표.md", "기타/b.md"},
		mergedPages:  []string{"업무/c.md"},
	}
	wd.recordDreamCycleChanges("hash1", cycle)
	wd.recordDreamCycleChanges("hash2", &dreamCycle{appliedPaths: []string{"기타/d.md"}})

	rec, ok := s.DreamCycleChangesByCommit("hash1")
	if !ok || len(rec.Learned) != 2 || rec.Merged[0] != "업무/c.md" {
		t.Fatalf("lookup hash1 = %+v ok=%v", rec, ok)
	}
	if _, ok := s.DreamCycleChangesByCommit("missing"); ok {
		t.Fatal("unknown commit must not resolve")
	}

	pages := DreamRevertPageList(rec)
	want := "프로젝트/a/대표.md|기타/b.md|업무/c.md"
	if strings.Join(pages, "|") != want {
		t.Fatalf("page list = %v, want canonical order %v", pages, want)
	}
	// A page present in two lists appears once (learned wins the slot).
	dup := DreamRevertPageList(DreamCycleChanges{
		Learned: []string{"x.md"}, Merged: []string{"x.md", "y.md"},
	})
	if strings.Join(dup, "|") != "x.md|y.md" {
		t.Fatalf("dedup order = %v", dup)
	}

	// The ledger file is read straight from the store dir — a fresh Store on the
	// same dir resolves too (feed actions run long after the dreamer is gone).
	if raw, err := os.ReadFile(filepath.Join(s.Dir(), ".dream-cycle-changes.jsonl")); err != nil || len(raw) == 0 {
		t.Fatalf("ledger file must exist in the store dir: %v", err)
	}
}
