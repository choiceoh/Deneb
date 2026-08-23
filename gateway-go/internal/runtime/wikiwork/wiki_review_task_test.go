package wikiwork

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

// TestWikiReviewRecentlyTouchedPagesReturnsDedupedNewestFirst: the audit-log parser returns touched
// pages newest-first, deduped, skipping raw-data buckets.
func TestWikiReviewRecentlyTouchedPagesReturnsDedupedNewestFirst(t *testing.T) {
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

func TestAppendObserved_DedupesByPayload(t *testing.T) {
	st := &wikiReviewState{}
	appendObserved(st, "2026-08-22 10:00 | mail-domain 프로젝트/메일분석/a@sunkean.com.md → nde-sun-cbl-001")
	appendObserved(st, "2026-08-23 12:00 | mail-domain 프로젝트/메일분석/a@sunkean.com.md → nde-sun-cbl-001")
	appendObserved(st, "2026-08-23 12:00 | mail-domain 프로젝트/메일분석/b@barocorp.com.md → pl1-sin-bes-001")
	if len(st.Observed) != 2 {
		t.Fatalf("Observed=%v want 2 unique payloads", st.Observed)
	}
}

// TestWikiReviewRunMergesDuplicateAndIgnoresInventedPath: end-to-end with auto-merge
// armed — two same-title pages, a fake verdict, and the duplicate is folded
// (reversibly) while an invented path in the verdict is ignored.
func TestWikiReviewRunMergesDuplicateAndIgnoresInventedPath(t *testing.T) {
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
		return `[
			{"page":"업무/탑솔라-공급-계약.md","duplicate_of":"업무/탑솔라-공급계약.md","confidence":"high"},
			{"page":"업무/탑솔라-공급-계약.md","duplicate_of":"업무/없는-문서.md","confidence":"high"}
		]`, nil
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

// TestWikiReviewGatherSuspectsRejectsSameProjectSlots: a project's 대표.md and detail
// pages must never be offered as duplicate candidates of each other.
func TestWikiReviewGatherSuspectsRejectsSameProjectSlots(t *testing.T) {
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

// TestWikiReviewMaintenanceRunsWhenCycleIsQuiet: the deterministic maintenance
// sweep (here proven via log rotation) must run even on a TRULY quiet cycle —
// touched==0, because the watermark is pre-advanced past the fixture writes.
// The regression this guards: rotation/dormancy/dead-link pruning/mail-refiling
// used to sit AFTER the duplicate-review early-returns (touched==0,
// suspects==0, verdict error), so on the common quiet cycle they never ran. On
// the old code this test fails (로그-보관.md never appears).
func TestWikiReviewMaintenanceRunsWhenCycleIsQuiet(t *testing.T) {
	task, store := newReviewFixture(t)
	// A lone project with an over-long 로그.md and no duplicate candidates.
	rep := wiki.NewPage("테스트프로젝트", "프로젝트", nil)
	rep.Body = "# 테스트프로젝트"
	if err := store.WritePage("프로젝트/테스트프로젝트/대표.md", rep); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString("# 테스트프로젝트 진행 로그\n\n")
	for i := 1; i <= wiki.LogKeepSections+5; i++ {
		fmt.Fprintf(&sb, "## 2026-01-%02d 회의\n내용 %d\n\n", i, i)
	}
	logPage := wiki.NewPage("테스트프로젝트 진행 로그", "프로젝트", nil)
	logPage.Body = sb.String()
	if err := store.WritePage("프로젝트/테스트프로젝트/로그.md", logPage); err != nil {
		t.Fatal(err)
	}
	// Advance the watermark past the fixture writes (the parser pulls the window
	// back one minute, hence the +2m) so this cycle genuinely sees touched==0.
	if err := task.saveState(&wikiReviewState{
		Version:      1,
		LastReviewMs: time.Now().Add(2 * time.Minute).UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	if got := task.recentlyTouchedPages(time.UnixMilli(task.loadState().LastReviewMs)); len(got) != 0 {
		t.Fatalf("precondition: touched must be empty, got %v", got)
	}

	// touched==0 → the verdict LLM must never fire.
	task.llm = func(_ context.Context, _, _ string, _ int) (string, error) {
		t.Error("verdict call must not fire on a quiet cycle")
		return "[]", nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Maintenance ran despite zero touched pages: the overflow sections
	// (beyond the newest LogKeepSections) moved into the archive page.
	if _, err := store.ReadPage(wiki.LogArchivePath("테스트프로젝트")); err != nil {
		t.Errorf("log rotation must run on a quiet cycle, but 로그-보관.md missing: %v", err)
	}
}

// TestWikiReviewGatherSuspectsFoldsFlatRemnantAndPreservesContent: when a blind write recreates
// the flat 프로젝트/<name>.md while the in-folder 대표.md already exists, MovePage
// fails (target exists) and the same-folder filter hides the pair from the
// duplicate review — so the layout repair must FOLD the remnant into the slot
// (deterministic; content survives under the merge marker).
func TestWikiReviewGatherSuspectsFoldsFlatRemnantAndPreservesContent(t *testing.T) {
	task, store := newReviewFixture(t)
	rep := wiki.NewPage("영산고", "프로젝트", nil)
	rep.Body = "# 영산고 태양광"
	if err := store.WritePage("프로젝트/영산고/대표.md", rep); err != nil {
		t.Fatal(err)
	}
	flat := wiki.NewPage("영산고", "프로젝트", nil)
	flat.Body = "# 영산고\n\n블라인드 RPC가 쓴 잔재 내용"
	if err := store.WritePage("프로젝트/영산고.md", flat); err != nil {
		t.Fatal(err)
	}

	task.gatherSuspects(context.Background(), []string{"프로젝트/영산고.md"})

	if _, err := store.ReadPage("프로젝트/영산고.md"); err == nil {
		t.Error("flat remnant must be folded away, but still exists")
	}
	merged, err := store.ReadPage("프로젝트/영산고/대표.md")
	if err != nil {
		t.Fatalf("rep slot unreadable after fold: %v", err)
	}
	if !strings.Contains(merged.Body, "블라인드 RPC가 쓴 잔재 내용") {
		t.Errorf("remnant content lost in fold:\n%s", merged.Body)
	}
}
