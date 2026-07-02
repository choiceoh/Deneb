package server

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func newReviewFixture(t *testing.T) (*wikiReviewTask, *wiki.Store) {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), "")
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	task := &wikiReviewTask{
		wikiStore: store,
		logger:    slog.Default(),
		statePath: filepath.Join(dir, "wiki-review-state.json"),
	}
	return task, store
}

func writeReviewPage(t *testing.T, store *wiki.Store, path, title string) {
	t.Helper()
	page := wiki.NewPage(title, "업무", nil)
	page.Body = "# " + title + "\n본문"
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("WritePage(%s): %v", path, err)
	}
}

// TestWikiReview_RecentlyTouchedPages: the audit-log parser returns touched
// pages newest-first, deduped, skipping raw-data buckets.
func TestWikiReview_RecentlyTouchedPages(t *testing.T) {
	task, store := newReviewFixture(t)
	writeReviewPage(t, store, "업무/구리값-동향.md", "구리값 동향")
	writeReviewPage(t, store, "업무/구리값-동향.md", "구리값 동향")                  // second write → update, dedup
	writeReviewPage(t, store, "프로젝트/영산고/메일분석/19e8717314b5c914.md", "메일") // raw data → skipped

	got := task.recentlyTouchedPages(time.UnixMilli(0))
	if len(got) != 1 || got[0] != "업무/구리값-동향.md" {
		t.Fatalf("recentlyTouchedPages = %v, want [업무/구리값-동향.md]", got)
	}

	// A high-water mark in the future filters everything out.
	if got := task.recentlyTouchedPages(time.Now().Add(2 * time.Minute)); len(got) != 0 {
		t.Fatalf("future since should return nothing, got %v", got)
	}
}

// TestWikiReview_ObserveModeRecordsWithoutMerging: the rollout default — a
// high-confidence verdict is recorded in the state audit trail, nothing merges.
func TestWikiReview_ObserveModeRecordsWithoutMerging(t *testing.T) {
	task, store := newReviewFixture(t) // autoMerge stays false
	writeReviewPage(t, store, "업무/탑솔라-공급계약.md", "탑솔라 공급 계약")
	writeReviewPage(t, store, "업무/탑솔라-공급-계약.md", "탑솔라 공급 계약")

	task.llm = func(_ context.Context, _, _ string, _ int) (string, error) {
		return `[{"page":"업무/탑솔라-공급-계약.md","duplicate_of":"업무/탑솔라-공급계약.md","confidence":"high"}]`, nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Both pages survive.
	if _, err := store.ReadPage("업무/탑솔라-공급계약.md"); err != nil {
		t.Errorf("observe mode must not merge: %v", err)
	}
	if _, err := store.ReadPage("업무/탑솔라-공급-계약.md"); err != nil {
		t.Errorf("observe mode must not merge: %v", err)
	}
	// The would-merge landed in the audit trail.
	st := task.loadState()
	if len(st.Observed) != 1 || !strings.Contains(st.Observed[0], "업무/탑솔라-공급-계약.md") {
		t.Errorf("observed audit trail = %v, want the recorded pair", st.Observed)
	}
}

// TestWikiReview_RunMergesHighConfidenceDuplicate: end-to-end with auto-merge
// armed — two same-title pages, a fake verdict, and the duplicate is folded
// (reversibly) while an invented path in the verdict is ignored.
func TestWikiReview_RunMergesHighConfidenceDuplicate(t *testing.T) {
	task, store := newReviewFixture(t)
	task.autoMerge = true
	writeReviewPage(t, store, "업무/탑솔라-공급계약.md", "탑솔라 공급 계약")
	writeReviewPage(t, store, "업무/탑솔라-공급-계약.md", "탑솔라 공급 계약")

	calls := 0
	task.llm = func(_ context.Context, _, user string, _ int) (string, error) {
		calls++
		if !strings.Contains(user, "업무/탑솔라-공급-계약.md") {
			t.Errorf("verdict prompt missing suspect page:\n%s", user)
		}
		return fmt.Sprintf(`[
			{"page":"업무/탑솔라-공급-계약.md","duplicate_of":"업무/탑솔라-공급계약.md","confidence":"high"},
			{"page":"업무/탑솔라-공급-계약.md","duplicate_of":"업무/없는-문서.md","confidence":"high"}
		]`), nil
	}

	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one verdict call, got %d", calls)
	}

	// One of the pair was folded into the other (keeper policy may pick either);
	// exactly one survives and it carries the merged marker.
	a, aErr := store.ReadPage("업무/탑솔라-공급계약.md")
	b, bErr := store.ReadPage("업무/탑솔라-공급-계약.md")
	if (aErr == nil) == (bErr == nil) {
		t.Fatalf("exactly one page should survive; aErr=%v bErr=%v", aErr, bErr)
	}
	survivor := a
	if aErr != nil {
		survivor = b
	}
	if !strings.Contains(survivor.Body, "병합된 중복 문서") {
		t.Errorf("survivor missing merge marker:\n%s", survivor.Body)
	}

	// Second run: high-water mark advanced past the writes, nothing to review —
	// but the fold itself logged an update; the surviving page has no candidates
	// left, so no verdict call fires.
	task.llm = func(_ context.Context, _, _ string, _ int) (string, error) {
		t.Error("no verdict call expected on the follow-up cycle")
		return "[]", nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
}

// TestWikiReview_SameProjectSlotsAreNotSuspects: a project's 대표.md and detail
// pages must never be offered as duplicate candidates of each other.
func TestWikiReview_SameProjectSlotsAreNotSuspects(t *testing.T) {
	task, store := newReviewFixture(t)
	rep := wiki.NewPage("영산고", "프로젝트", nil)
	rep.Body = "# 영산고 태양광 사업"
	if err := store.WritePage("프로젝트/영산고/대표.md", rep); err != nil {
		t.Fatal(err)
	}
	logPage := wiki.NewPage("영산고 진행 로그", "프로젝트", nil)
	logPage.Body = "# 영산고 진행 로그"
	if err := store.WritePage("프로젝트/영산고/로그.md", logPage); err != nil {
		t.Fatal(err)
	}

	suspects := task.gatherSuspects(context.Background(),
		[]string{"프로젝트/영산고/로그.md", "프로젝트/영산고/대표.md"})
	for _, s := range suspects {
		for _, c := range s.candidates {
			if strings.HasPrefix(c.Path, "프로젝트/영산고/") {
				t.Errorf("same-project slot offered as duplicate candidate: %s → %s", s.path, c.Path)
			}
		}
	}
}
