package wiki

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newCloseStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })
	return store
}

func seedProject(t *testing.T, store *Store, name string, extraPages ...string) {
	t.Helper()
	rep := NewPage(name, "프로젝트", nil)
	rep.Body = "# " + name + "\n\n## 현재 상태\n- 진행 중\n"
	if err := store.WritePage(RepPagePath(name), rep); err != nil {
		t.Fatal(err)
	}
	for _, p := range extraPages {
		page := NewPage(p, "프로젝트", nil)
		page.Body = "# " + p
		if err := store.WritePage("프로젝트/"+name+"/"+p+".md", page); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCloseAndReopenProject: closure records the outcome, archives the whole
// folder, and retires the project from the active stage; reopen restores all.
func TestCloseAndReopenProject(t *testing.T) {
	store := newCloseStore(t)
	seedProject(t, store, "영산고", "로그", "기자재-케이블")
	seedProject(t, store, "부산8호")

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	res, err := store.CloseProject("영산고", "수주 실패 — SKNS 낙찰", now)
	if err != nil {
		t.Fatalf("CloseProject: %v", err)
	}
	if res.Archived != 3 {
		t.Errorf("archived = %d, want 3 (대표+로그+기자재)", res.Archived)
	}

	// 종결 record on the 대표페이지, and every folder page archived.
	rep := testutil.Must(store.ReadPage(RepPagePath("영산고")))
	if !rep.Meta.Archived || !strings.Contains(rep.Section("종결"), "수주 실패") {
		t.Errorf("rep after close: archived=%v section=%q", rep.Meta.Archived, rep.Section("종결"))
	}
	logPage := testutil.Must(store.ReadPage("프로젝트/영산고/로그.md"))
	if !logPage.Meta.Archived {
		t.Error("folder page must be archived on close")
	}

	// Retired from the active stage: KnownProjects lists only 부산8호.
	refs := store.KnownProjects()
	if len(refs) != 1 || refs[0].Name != "부산8호" {
		t.Fatalf("KnownProjects after close = %+v, want only 부산8호", refs)
	}

	// Reopen restores everything and keeps the closure history visible.
	rres, err := store.ReopenProject("영산고", now.AddDate(0, 1, 0))
	if err != nil {
		t.Fatalf("ReopenProject: %v", err)
	}
	if rres.Restored != 3 {
		t.Errorf("restored = %d, want 3", rres.Restored)
	}
	rep = testutil.Must(store.ReadPage(RepPagePath("영산고")))
	if rep.Meta.Archived {
		t.Error("rep must be active after reopen")
	}
	if sec := rep.Section("종결"); !strings.Contains(sec, "재개") || !strings.Contains(sec, "수주 실패") {
		t.Errorf("종결 history should keep close+reopen lines, got %q", sec)
	}
	if refs := store.KnownProjects(); len(refs) != 2 {
		t.Errorf("KnownProjects after reopen = %d, want 2", len(refs))
	}

	// Missing project errors clearly.
	if _, err := store.CloseProject("없는-프로젝트", "", now); err == nil {
		t.Error("closing a missing project must error")
	}
}

// TestReopenProject_KeepsLogArchiveArchived: reopening a project un-archives its
// pages EXCEPT the rotated-log archive (로그-보관.md), which RotateProjectLog
// archived on purpose. Un-archiving it would resurface old rotated-out log
// sections in search — so it must stay archived across a close/reopen.
func TestReopenProject_KeepsLogArchiveArchived(t *testing.T) {
	store := newCloseStore(t)
	seedProject(t, store, "가덕도")

	// Over-long 로그.md → rotate → 로그-보관.md (archived by rotation, not closure).
	var sb strings.Builder
	for i := 1; i <= LogKeepSections+3; i++ {
		fmt.Fprintf(&sb, "## 2026-01-%02d 회의\n메모 %d\n\n", i, i)
	}
	logPage := NewPage("가덕도 로그", "프로젝트", nil)
	logPage.Body = sb.String()
	if err := store.WritePage(LogPagePath("가덕도"), logPage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateProjectLog("가덕도"); err != nil {
		t.Fatalf("RotateProjectLog: %v", err)
	}
	if arc := testutil.Must(store.ReadPage(LogArchivePath("가덕도"))); !arc.Meta.Archived {
		t.Fatal("precondition: rotated 로그-보관.md must be archived")
	}

	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	if _, err := store.CloseProject("가덕도", "보류", now); err != nil {
		t.Fatalf("CloseProject: %v", err)
	}
	if _, err := store.ReopenProject("가덕도", now.AddDate(0, 1, 0)); err != nil {
		t.Fatalf("ReopenProject: %v", err)
	}

	if rep := testutil.Must(store.ReadPage(RepPagePath("가덕도"))); rep.Meta.Archived {
		t.Error("rep must be active after reopen")
	}
	if arc := testutil.Must(store.ReadPage(LogArchivePath("가덕도"))); !arc.Meta.Archived {
		t.Error("로그-보관.md must stay archived after reopen (rotation artifact, not closure)")
	}
}

// TestCloseProject_LegacyFlatRep: a legacy flat 대표페이지 (pre-migration form)
// closes the same way.
func TestCloseProject_LegacyFlatRep(t *testing.T) {
	store := newCloseStore(t)
	rep := NewPage("옛날사업", "프로젝트", nil)
	rep.Body = "# 옛날사업"
	if err := store.WritePage("프로젝트/옛날사업.md", rep); err != nil {
		t.Fatal(err)
	}

	res, err := store.CloseProject("프로젝트/옛날사업.md", "완공", time.Now())
	if err != nil {
		t.Fatalf("CloseProject(legacy): %v", err)
	}
	if res.RepPath != "프로젝트/옛날사업.md" || res.Archived != 1 {
		t.Errorf("res = %+v", res)
	}
	if refs := store.KnownProjects(); len(refs) != 0 {
		t.Errorf("closed legacy project still listed: %+v", refs)
	}
}

// TestCloseProject_ReservedBucketPathRejected: the 거래 ledger (and any other
// reserved-bucket page) must never resolve as a project rep — the old fallback
// name "거래/탑솔라" slipped past isReservedProjectDir and the legacy lookup
// then archived the ledger itself.
func TestCloseProject_ReservedBucketPathRejected(t *testing.T) {
	store := newCloseStore(t)
	ledger := NewPage("탑솔라", "프로젝트", nil)
	ledger.Body = "# 탑솔라 거래 원장"
	if err := store.WritePage("프로젝트/거래/탑솔라.md", ledger); err != nil {
		t.Fatal(err)
	}

	for _, ref := range []string{"프로젝트/거래/탑솔라.md", "프로젝트/거래/탑솔라", `프로젝트\거래\탑솔라.md`} {
		if _, err := store.CloseProject(ref, "", time.Now()); err == nil {
			t.Errorf("CloseProject(%q) must error — reserved bucket page is not a project", ref)
		}
	}
	if got := testutil.Must(store.ReadPage("프로젝트/거래/탑솔라.md")); got.Meta.Archived {
		t.Error("거래 ledger was archived by a rejected close")
	}
}

// TestCloseProject_RecloseCountsNoNewArchives: re-closing refreshes the 종결
// record but must report 0 newly archived pages (the flag didn't flip).
func TestCloseProject_RecloseCountsNoNewArchives(t *testing.T) {
	store := newCloseStore(t)
	seedProject(t, store, "영산고", "로그")
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)

	first := testutil.Must(store.CloseProject("영산고", "보류", now))
	if first.Archived != 2 {
		t.Fatalf("first close archived = %d, want 2", first.Archived)
	}
	again := testutil.Must(store.CloseProject("영산고", "최종 종결", now.AddDate(0, 0, 1)))
	if again.Archived != 0 {
		t.Errorf("re-close archived = %d, want 0 (nothing newly flagged)", again.Archived)
	}
}

// TestReopenProject_ActiveProjectErrors: reopening a project that was never
// closed must return the "이미 활성" error — and must not stamp a spurious 재개
// line or touch Updated.
func TestReopenProject_ActiveProjectErrors(t *testing.T) {
	store := newCloseStore(t)
	seedProject(t, store, "영산고")
	before := testutil.Must(store.ReadPage(RepPagePath("영산고")))

	res, err := store.ReopenProject("영산고", time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "이미 활성") {
		t.Fatalf("ReopenProject(active) = (%+v, %v), want 이미 활성 error", res, err)
	}
	after := testutil.Must(store.ReadPage(RepPagePath("영산고")))
	if strings.Contains(after.Body, "재개") {
		t.Errorf("spurious 재개 history on an active project:\n%s", after.Body)
	}
	if after.Meta.Updated != before.Meta.Updated {
		t.Errorf("Updated churned by a rejected reopen: %q → %q", before.Meta.Updated, after.Meta.Updated)
	}
}

// TestCloseProject_ByDisplayTitle: a project addressable by its page Title even
// when the Title differs from the folder name.
func TestCloseProject_ByDisplayTitle(t *testing.T) {
	store := newCloseStore(t)
	rep := NewPage("기아 화성", "프로젝트", nil) // display title with a space
	rep.Body = "# 기아 화성"
	if err := store.WritePage(RepPagePath("기아-화성"), rep); err != nil { // folder name differs
		t.Fatal(err)
	}

	res, err := store.CloseProject("기아 화성", "완료", time.Now())
	if err != nil {
		t.Fatalf("CloseProject(by title): %v", err)
	}
	if res.RepPath != RepPagePath("기아-화성") {
		t.Errorf("resolved rep = %q, want %q", res.RepPath, RepPagePath("기아-화성"))
	}
}

// TestFlagDormantProjects: long-inactive active projects get one 종결-검토
// bullet (quarter-idempotent, capped); fresh and closed projects don't.
func TestFlagDormantProjects(t *testing.T) {
	store := newCloseStore(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -(dormantAfterDays + 40)).Format("2006-01-02")

	makeStale := func(name string) {
		t.Helper()
		seedProject(t, store, name)
		if err := store.UpdatePage(RepPagePath(name), func(cur *Page) (*Page, error) {
			cur.Meta.Updated = old
			return cur, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	makeStale("졸린사업A")
	makeStale("졸린사업B")
	makeStale("종결된사업")
	seedProject(t, store, "활발사업") // fresh Updated (today)

	if _, err := store.CloseProject("종결된사업", "", now); err != nil {
		t.Fatal(err)
	}

	flagged := store.FlagDormantProjects(now, 10)
	if len(flagged) != 2 {
		t.Fatalf("flagged = %v, want the two dormant active projects", flagged)
	}
	rep := testutil.Must(store.ReadPage(RepPagePath("졸린사업A")))
	if !strings.Contains(rep.Body, "종결 검토 제안") {
		t.Errorf("dormant bullet missing:\n%s", rep.Body)
	}

	// Quarter idempotency: a second pass flags nothing new (and the flag itself
	// refreshed Updated, so they are no longer dormant anyway).
	if again := store.FlagDormantProjects(now.Add(time.Hour), 10); len(again) != 0 {
		t.Errorf("second pass flagged again: %v", again)
	}
}

// TestFlagDormantProjects_Cap: the per-call cap holds.
func TestFlagDormantProjects_Cap(t *testing.T) {
	store := newCloseStore(t)
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -(dormantAfterDays + 40)).Format("2006-01-02")
	for _, name := range []string{"a사업", "b사업", "c사업"} {
		seedProject(t, store, name)
		if err := store.UpdatePage(RepPagePath(name), func(cur *Page) (*Page, error) {
			cur.Meta.Updated = old
			return cur, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if flagged := store.FlagDormantProjects(now, 2); len(flagged) != 2 {
		t.Fatalf("flagged = %v, want cap 2", flagged)
	}
}
