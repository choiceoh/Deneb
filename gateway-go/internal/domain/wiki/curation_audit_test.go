// curation_audit_test.go — regression tests for the dreamer + curation audit
// follow-up (2026-07): mail-analysis merge exclusion, cross-project rep-pair
// demotion, archived-aware keeper choice, synthesis parse salvage, batch
// clobber guard, idempotent log reroute, action drift, diary ledger recovery,
// slot-leaf link pruning, and lifecycle clock preservation.
package wiki

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newAuditStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newAuditDreamer(t *testing.T) (*WikiDreamer, *Store) {
	t.Helper()
	store := newAuditStore(t)
	return NewWikiDreamer(store, nil, "", Config{}, discardLogger()), store
}

// --- D2: detectDuplicates excludes mail-analysis / raw-data / log / archived ---

// TestDetectDuplicates_ExcludesMailRawLogArchived: same-subject thread mails
// ("RE: 견적 요청") share normalized titles; auto-folding them deleted mail pages
// (메일 1통=1페이지 계약 파괴). Raw-data, log slots, and archived pages must never
// be duplicate candidates — mirroring the wiki reviewer's exclusions.
func TestDetectDuplicates_ExcludesMailRawLogArchived(t *testing.T) {
	entries := map[string]IndexEntry{
		// Two same-subject mails in different projects' 메일분석 slots.
		"프로젝트/영산고/메일분석/m1.md":  {Title: "RE: 견적 요청"},
		"프로젝트/부산8호/메일분석/m2.md": {Title: "RE 견적 요청"},
		// Unlinked bucket + legacy bucket + deal ledger.
		"프로젝트/메일분석/m3.md":          {Title: "RE: 견적 요청"},
		"프로젝트/mail-analyses/m4.md": {Title: "RE: 견적 요청"},
		"프로젝트/거래/한빛전기.md":          {Title: "한빛전기 거래"},
		"프로젝트/거래/한빛 전기.md":         {Title: "한빛 전기 거래"},
		// Log slots share shape across projects.
		"프로젝트/영산고/로그.md":    {Title: "영산고 진행 로그"},
		"프로젝트/영산고/로그-보관.md": {Title: "영산고 로그 보관"},
		// Archived duplicate of a live page.
		"기타/보관된중복.md": {Title: "중복 주제", Archived: true},
		"기타/살아있는.md":  {Title: "중복 주제"},
	}
	findings := detectDuplicates(entries)
	if len(findings) != 0 {
		t.Fatalf("excluded page classes must produce no duplicate findings, got %+v", findings)
	}

	// Positive control: two curated pages with the same normalized title still
	// produce a merge Fix.
	entries["기타/살아있는2.md"] = IndexEntry{Title: "중복-주제"}
	findings = detectDuplicates(entries)
	if len(findings) != 1 || findings[0].Fix == nil || findings[0].Fix.Kind != "merge" {
		t.Fatalf("curated duplicate pair must keep its merge Fix, got %+v", findings)
	}
}

// --- C3: cross-project rep-pair demoted to advisory + FoldDuplicate guard ---

// TestDetectDuplicates_CrossProjectRepPairIsAdvisory: two projects with
// normalization-identical names produce a rep pair whose auto-fold would
// orphan the folded project's children — the finding must stay advisory.
func TestDetectDuplicates_CrossProjectRepPairIsAdvisory(t *testing.T) {
	entries := map[string]IndexEntry{
		"프로젝트/기아-화성/대표.md": {Title: "기아 화성"},
		"프로젝트/기아 화성/대표.md": {Title: "기아-화성"},
	}
	findings := detectDuplicates(entries)
	if len(findings) != 1 {
		t.Fatalf("expected one advisory finding, got %+v", findings)
	}
	if findings[0].Fix != nil {
		t.Fatalf("cross-project rep pair must not carry an auto-merge Fix: %+v", findings[0])
	}

	// Same-folder pair (flat remnant + its 대표.md slot) stays auto-foldable —
	// that fold is a layout repair, not a cross-project merge.
	entries = map[string]IndexEntry{
		"프로젝트/기아-화성.md":    {Title: "기아 화성"},
		"프로젝트/기아-화성/대표.md": {Title: "기아 화성"},
	}
	findings = detectDuplicates(entries)
	if len(findings) != 1 || findings[0].Fix == nil {
		t.Fatalf("same-project flat+slot pair must keep its merge Fix, got %+v", findings)
	}
}

// TestFoldDuplicate_RefusesCrossProjectRepPair: the shared automatic-fold
// primitive (dream verify + reviewer) refuses to fold one project's 대표페이지
// into another project's — children would need re-parenting first.
func TestFoldDuplicate_RefusesCrossProjectRepPair(t *testing.T) {
	store := newAuditStore(t)
	for _, name := range []string{"기아-화성", "기아 화성"} {
		rep := NewPage(name, "프로젝트", nil)
		rep.Body = "# " + name
		if err := store.WritePage(RepPagePath(name), rep); err != nil {
			t.Fatal(err)
		}
		log := NewPage(name+" 진행 로그", "프로젝트", nil)
		log.Body = "## 2026-07-01\n회의"
		if err := store.WritePage(LogPagePath(name), log); err != nil {
			t.Fatal(err)
		}
	}
	err := store.FoldDuplicate(RepPagePath("기아-화성"), RepPagePath("기아 화성"))
	if err == nil {
		t.Fatal("expected cross-project rep fold to be refused")
	}
	// Both projects intact.
	for _, name := range []string{"기아-화성", "기아 화성"} {
		if _, rerr := store.ReadPage(RepPagePath(name)); rerr != nil {
			t.Fatalf("rep page of %s must survive: %v", name, rerr)
		}
	}

	// The same-folder layout fold (flat remnant → slot) still works.
	flat := NewPage("영산고", "프로젝트", nil)
	flat.Body = "# 영산고 (flat)"
	if err := store.WritePage("프로젝트/영산고.md", flat); err != nil {
		t.Fatal(err)
	}
	slot := NewPage("영산고", "프로젝트", nil)
	slot.Body = "# 영산고 (slot)"
	if err := store.WritePage(RepPagePath("영산고"), slot); err != nil {
		t.Fatal(err)
	}
	if err := store.FoldDuplicate(RepPagePath("영산고"), "프로젝트/영산고.md"); err != nil {
		t.Fatalf("same-project flat→slot fold must stay allowed: %v", err)
	}
}

// --- C6: archived-aware keeper choice + index Archived persistence ---

// TestChooseDuplicateKeeper_PrefersLivePage: a live page never folds into an
// archived one, regardless of importance/Updated.
func TestChooseDuplicateKeeper_PrefersLivePage(t *testing.T) {
	store := newAuditStore(t)

	archived := NewPage("보관 페이지", "기타", nil)
	archived.Meta.Archived = true
	archived.Meta.Importance = 0.9
	archived.Meta.Updated = "2026-07-03"
	if err := store.WritePage("기타/보관.md", archived); err != nil {
		t.Fatal(err)
	}
	live := NewPage("살아있는 페이지", "기타", nil)
	live.Meta.Importance = 0.3
	live.Meta.Updated = "2026-01-01"
	if err := store.WritePage("기타/살아있음.md", live); err != nil {
		t.Fatal(err)
	}

	keep, fold := store.ChooseDuplicateKeeper("기타/보관.md", "기타/살아있음.md")
	if keep != "기타/살아있음.md" || fold != "기타/보관.md" {
		t.Fatalf("live page must be the keeper: keep=%q fold=%q", keep, fold)
	}
	// Argument order must not matter.
	keep, fold = store.ChooseDuplicateKeeper("기타/살아있음.md", "기타/보관.md")
	if keep != "기타/살아있음.md" || fold != "기타/보관.md" {
		t.Fatalf("live page must be the keeper regardless of order: keep=%q fold=%q", keep, fold)
	}
	// Both archived → the ordinary importance rule resumes.
	if err := store.UpdatePage("기타/살아있음.md", func(cur *Page) (*Page, error) {
		cur.Meta.Archived = true
		return cur, nil
	}); err != nil {
		t.Fatal(err)
	}
	if keep, _ = store.ChooseDuplicateKeeper("기타/보관.md", "기타/살아있음.md"); keep != "기타/보관.md" {
		t.Fatalf("both archived → higher importance wins, got keep=%q", keep)
	}
}

// TestIndexEntry_ArchivedSurvivesRestart: the Archived flag rides the index.md
// TSV (like related/created) so retirement-aware curation isn't blind after a
// gateway restart.
func TestIndexEntry_ArchivedSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	store := testutil.Must(NewStore(wikiDir, ""))

	page := NewPage("보관 대상", "기타", nil)
	page.Meta.Archived = true
	if err := store.WritePage("기타/보관대상.md", page); err != nil {
		t.Fatal(err)
	}
	live := NewPage("현역", "기타", nil)
	if err := store.WritePage("기타/현역.md", live); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLastProcessedAndSave(""); err != nil { // persist index.md
		t.Fatal(err)
	}
	_ = store.Close()

	reopened := testutil.Must(NewStore(wikiDir, ""))
	defer reopened.Close()
	entries := reopened.SnapshotEntries()
	if !entries["기타/보관대상.md"].Archived {
		t.Fatal("Archived flag lost across restart (index.md column missing?)")
	}
	if entries["기타/현역.md"].Archived {
		t.Fatal("live page must not reload as archived")
	}
}

// --- D3: parseWikiUpdates double-wrap + all-skipped backpressure ---

func TestParseWikiUpdates_UnwrapsDoubleWrappedArray(t *testing.T) {
	text := `[[{"action":"create","path":"기타/a.md","title":"A"},{"action":"update","path":"기타/b.md","title":"B"}]]`
	updates, partial, err := parseWikiUpdates(text, discardLogger())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if partial {
		t.Fatal("a fully-recovered double-wrap must not report partial")
	}
	if len(updates) != 2 || updates[0].Title != "A" || updates[1].Title != "B" {
		t.Fatalf("updates = %+v, want the two inner items", updates)
	}

	// A single normal object element must NOT be unwrapped.
	updates, _, err = parseWikiUpdates(`[{"action":"create","path":"기타/c.md","title":"C"}]`, discardLogger())
	if err != nil || len(updates) != 1 || updates[0].Title != "C" {
		t.Fatalf("single-object array mishandled: %+v, %v", updates, err)
	}
}

func TestParseWikiUpdates_AllItemsSkippedReportsPartial(t *testing.T) {
	// Array parses, but no element is a wikiUpdate → nothing consumed. The
	// caller must hold input cursors (partial=true), not advance past the
	// cycle's input.
	updates, partial, err := parseWikiUpdates(`[123, 456]`, discardLogger())
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("updates = %+v, want none", updates)
	}
	if !partial {
		t.Fatal("all-skipped batch must report partial=true (hold offsets)")
	}

	// One good + one bad item → not partial (the good item consumed input).
	updates, partial, err = parseWikiUpdates(`[{"action":"create","path":"기타/a.md","title":"A"}, 123]`, discardLogger())
	if err != nil || len(updates) != 1 {
		t.Fatalf("mixed batch mishandled: %+v, %v", updates, err)
	}
	if partial {
		t.Fatal("a batch with applied items must not report partial")
	}
}

// --- D4: two creates for the same path in one batch must merge, not clobber ---

func TestApplyUpdates_RepeatedCreateSamePathMerges(t *testing.T) {
	wd, store := newAuditDreamer(t)
	created, updated, _ := wd.applyUpdates(context.Background(), []wikiUpdate{
		{Action: "create", Path: "기타/메모.md", Title: "배치 메모 1", Content: "첫 번째 배치 아이템의 고유한 내용입니다."},
		{Action: "create", Path: "기타/메모.md", Title: "배치 메모 2", Content: "두 번째 배치 아이템의 고유한 내용입니다."},
	})
	page := testutil.Must(store.ReadPage("기타/메모.md"))
	if !strings.Contains(page.Body, "첫 번째 배치 아이템의 고유한 내용입니다.") {
		t.Fatalf("first create's content clobbered:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "두 번째 배치 아이템의 고유한 내용입니다.") {
		t.Fatalf("second item's content missing:\n%s", page.Body)
	}
	if created != 1 || updated != 1 {
		t.Fatalf("created=%d updated=%d, want 1/1 (second create counted as update)", created, updated)
	}
}

// --- D5: appendProjectLog is idempotent by content ---

func TestAppendProjectLog_IdempotentOnReconsumption(t *testing.T) {
	wd, store := newAuditDreamer(t)
	entry := "- 2026-07-03 김부장 미팅: 모듈 단가 합의"
	if !wd.appendProjectLog("영산고", entry) {
		t.Fatal("first append must persist")
	}
	// Held-offset re-consumption replays the same input → same entry again.
	if !wd.appendProjectLog("영산고", entry) {
		t.Fatal("re-append of an already-present entry still counts as persisted")
	}
	page := testutil.Must(store.ReadPage(LogPagePath("영산고")))
	if n := strings.Count(page.Body, "모듈 단가 합의"); n != 1 {
		t.Fatalf("entry stacked %d times, want exactly 1:\n%s", n, page.Body)
	}
}

// --- D6: unknown action drift writes as update; supersede points at a real page ---

func TestApplyUpdates_UnknownActionWritesBeforeSupersede(t *testing.T) {
	wd, store := newAuditDreamer(t)
	old := NewPage("구 문서", "기타", nil)
	old.Body = "# 구 문서"
	if err := store.WritePage("기타/구문서.md", old); err != nil {
		t.Fatal(err)
	}

	created, updated, _ := wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action:     "replace", // LLM drift — not a known action
		Path:       "기타/새문서.md",
		Title:      "새 문서",
		Content:    "대체된 최신 내용입니다.",
		Supersedes: flexStringList{"기타/구문서.md"},
	}})
	if created+updated != 1 {
		t.Fatalf("drift action must still write: created=%d updated=%d", created, updated)
	}
	// The superseding page must actually exist…
	if _, err := store.ReadPage("기타/새문서.md"); err != nil {
		t.Fatalf("supersede target was never written: %v", err)
	}
	// …and the old page points at it.
	oldPage := testutil.Must(store.ReadPage("기타/구문서.md"))
	if oldPage.Meta.SupersededBy != "기타/새문서.md" {
		t.Fatalf("SupersededBy = %q, want 기타/새문서.md", oldPage.Meta.SupersededBy)
	}
}

// --- D7: misclassification listing exclusions + cap ---

func TestMisclassificationLines_ExcludesRawDataAndCaps(t *testing.T) {
	entries := map[string]IndexEntry{
		"기타/일반문서.md":               {Title: "일반", Category: "기타"},
		"프로젝트/영산고/대표.md":           {Title: "영산고", Category: "프로젝트"},
		"프로젝트/영산고/메일분석/m1.md":      {Title: "RE: 견적", Category: "프로젝트"},
		"프로젝트/메일분석/m2.md":          {Title: "RE: 견적", Category: "프로젝트"},
		"프로젝트/mail-analyses/m3.md": {Title: "RE: 견적", Category: "프로젝트"},
		"프로젝트/거래/한빛전기.md":          {Title: "한빛전기", Category: "프로젝트"},
		"프로젝트/영산고/로그.md":           {Title: "로그", Category: "프로젝트"},
		"기타/보관됨.md":                {Title: "보관", Category: "기타", Archived: true},
	}
	lines := misclassificationLines(entries)
	if len(lines) != 2 {
		t.Fatalf("lines = %v, want only the curated live pages", lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "메일분석") || strings.Contains(l, "거래/") ||
			strings.Contains(l, "로그.md") || strings.Contains(l, "보관됨") {
			t.Fatalf("excluded page leaked into the prompt listing: %q", l)
		}
	}

	// Cap: a big index must not blow the prompt.
	big := make(map[string]IndexEntry, misclassifyMaxLines+50)
	for i := 0; i < misclassifyMaxLines+50; i++ {
		big[fmt.Sprintf("기타/페이지-%d.md", i)] = IndexEntry{Title: "T", Category: "기타"}
	}
	if got := len(misclassificationLines(big)); got != misclassifyMaxLines {
		t.Fatalf("cap not applied: got %d lines, want %d", got, misclassifyMaxLines)
	}
}

// --- D9: corrupt diary ledger must not seal older files via the legacy cutoff ---

func TestScanDiaries_CorruptStateIgnoresLegacyCutoff(t *testing.T) {
	wd, store := newAuditDreamer(t)
	diaryDir := store.DiaryDir()
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// An old diary file, older than the legacy cutoff.
	if err := os.WriteFile(filepath.Join(diaryDir, "diary-2026-06-01.md"), []byte("아직 소화 안 된 오래된 일지\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SetLastProcessedAndSave("2026-07-01"); err != nil {
		t.Fatal(err)
	}
	// Corrupt the per-file offset ledger.
	if err := os.WriteFile(wd.diaryProcessStatePath(), []byte("{corrupt json"), 0o600); err != nil {
		t.Fatal(err)
	}

	scan, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan == nil || !strings.Contains(scan.Content, "아직 소화 안 된 오래된 일지") {
		t.Fatal("corrupt ledger + legacy cutoff sealed an unconsumed older diary file")
	}
}

// --- D10: same-size rewrite must re-enter consumption ---

func TestScanDiaries_SameSizeRewriteReconsumed(t *testing.T) {
	wd, store := newAuditDreamer(t)
	diaryDir := store.DiaryDir()
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "diary-2026-07-03.md"
	path := filepath.Join(diaryDir, name)
	first := "가나다라 첫 번째 버전의 일지\n"
	if err := os.WriteFile(path, []byte(first), 0o644); err != nil {
		t.Fatal(err)
	}

	scan, err := wd.scanDiaries(context.Background())
	if err != nil || scan == nil {
		t.Fatalf("first scan: %v / %v", scan, err)
	}
	if err := wd.saveDiaryProcessState(scan.State); err != nil {
		t.Fatal(err)
	}

	// Same-length rewrite with a different mtime.
	second := "마바사아 두 번째 버전의 일지\n"
	if len(second) != len(first) {
		t.Fatalf("test setup: rewrite must be same-size (%d vs %d)", len(second), len(first))
	}
	if err := os.WriteFile(path, []byte(second), 0o644); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(path, newTime, newTime); err != nil {
		t.Fatal(err)
	}

	scan2, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if scan2 == nil || !strings.Contains(scan2.Content, "두 번째 버전의 일지") {
		t.Fatal("same-size rewrite was never re-consumed (offset==size skip)")
	}
}

// --- D12: recategorizedPath never moves project-owned paths ---

func TestRecategorizedPath_ProjectSlotPagesNeverMove(t *testing.T) {
	for _, p := range []string{
		"프로젝트/기아-화성/대표.md",
		"프로젝트/기아-화성/로그.md",
		"프로젝트/기아-화성/메일분석/m1.md",
		"프로젝트/기아-화성/기자재/모듈.md",
		"프로젝트/기아-화성.md", // legacy flat rep
	} {
		if got := recategorizedPath(p, "인물"); got != "" {
			t.Fatalf("recategorizedPath(%q) = %q, want \"\" (project-owned)", p, got)
		}
	}
	// Non-project paths keep the ordinary move behavior.
	if got := recategorizedPath("기타/석문호-저수지.md", "업무"); got != "업무/석문호-저수지.md" {
		t.Fatalf("ordinary move broken: %q", got)
	}
}

// --- D13: RunDream recovers a panicking cycle and backs off ---

func TestRunDream_PanicRecoversAndBacksOff(t *testing.T) {
	// nil store makes scanDiaries dereference a nil *Store → panic.
	wd := &WikiDreamer{logger: discardLogger()}
	wd.lastDream = time.Now().Add(-24 * time.Hour)
	if !wd.ShouldDream(context.Background()) {
		t.Fatal("precondition: ShouldDream must be true")
	}

	report, err := wd.RunDream(context.Background())
	if err == nil || !strings.Contains(err.Error(), "panic") {
		t.Fatalf("panicking cycle must surface as an error, got report=%v err=%v", report, err)
	}
	// The backoff (resetCounters) must have advanced lastDream so the 30-min
	// timer doesn't hot-loop the same doomed cycle.
	if wd.ShouldDream(context.Background()) {
		t.Fatal("panicked cycle must back off one interval (resetCounters)")
	}
}

// --- C4: unique-basename link repair must not treat slot leaves as identity ---

func TestPruneDeadRelatedLinks_SlotLeafNeverRepointsAcrossProjects(t *testing.T) {
	store := newAuditStore(t)
	// Exactly ONE project on disk → byBasename["대표.md"] would be "unique".
	rep := NewPage("영산고", "프로젝트", nil)
	if err := store.WritePage(RepPagePath("영산고"), rep); err != nil {
		t.Fatal(err)
	}
	referrer := NewPage("참조 문서", "기타", nil)
	referrer.Meta.Related = []string{"프로젝트/사라진프로젝트/대표.md"} // dead slot ref
	if err := store.WritePage("기타/참조.md", referrer); err != nil {
		t.Fatal(err)
	}

	if _, err := store.PruneDeadRelatedLinks(); err != nil {
		t.Fatal(err)
	}
	got := testutil.Must(store.ReadPage("기타/참조.md"))
	for _, r := range got.Meta.Related {
		if r == RepPagePath("영산고") {
			t.Fatalf("dead slot ref repointed to an UNRELATED project's slot: %v", got.Meta.Related)
		}
	}
	if len(got.Meta.Related) != 0 {
		t.Fatalf("dead slot ref should be dropped, got %v", got.Meta.Related)
	}
}

// --- C8: lifecycle clocks — archivePage keeps Updated; reclassify skips archived ---

func TestArchivePage_PreservesUpdated(t *testing.T) {
	store := newAuditStore(t)
	page := NewPage("오래된 메일", "프로젝트", nil)
	page.Meta.Updated = "2026-01-01"
	if err := store.WritePage("프로젝트/메일분석/m-old.md", page); err != nil {
		t.Fatal(err)
	}
	if err := store.archivePage("프로젝트/메일분석/m-old.md"); err != nil {
		t.Fatal(err)
	}
	got := testutil.Must(store.ReadPage("프로젝트/메일분석/m-old.md"))
	if !got.Meta.Archived {
		t.Fatal("page must be archived")
	}
	if got.Meta.Updated != "2026-01-01" {
		t.Fatalf("archiving must preserve Updated (metadata-only repair), got %q", got.Meta.Updated)
	}
}

func TestReclassify_SkipsArchivedAndDoesNotStampUpdated(t *testing.T) {
	store := newReclassifyStore(t)

	// Archived unlinked mail with a clear project signal → must stay put.
	archived := NewPage("RE: 견적 회신", "프로젝트", nil)
	archived.Meta.Type = "log"
	archived.Meta.Archived = true
	archived.Meta.Related = []string{"프로젝트/기아-화성/대표.md"}
	if err := store.WritePage(MailAnalysisPagePath("", "arch1"), archived); err != nil {
		t.Fatal(err)
	}
	// Live mail with the same signal and an old Updated stamp.
	live := NewPage("기아 화성 발주 확정", "프로젝트", nil)
	live.Meta.Type = "log"
	live.Meta.Updated = "2026-02-02"
	live.Meta.Related = []string{"프로젝트/기아-화성/대표.md"}
	if err := store.WritePage(MailAnalysisPagePath("", "live1"), live); err != nil {
		t.Fatal(err)
	}

	moved := store.ReclassifyUnlinkedMailAnalyses(10)
	if len(moved) != 1 || moved[0].Project != "기아-화성" {
		t.Fatalf("moved = %+v, want only the live mail", moved)
	}
	if _, err := store.ReadPage(MailAnalysisPagePath("", "arch1")); err != nil {
		t.Fatalf("archived mail must stay in the unlinked bucket: %v", err)
	}
	// The re-file is a metadata repair: Updated must NOT be re-stamped
	// (stamping extended the 90d retention window and reset dormancy clocks).
	refiled := testutil.Must(store.ReadPage(MailAnalysisPagePath("기아-화성", "live1")))
	if refiled.Meta.Updated != "2026-02-02" {
		t.Fatalf("re-file must preserve Updated, got %q", refiled.Meta.Updated)
	}
}

// --- C11: global msgID lookup for the one-page-per-message contract ---

func TestFindMailAnalysisPage(t *testing.T) {
	store := newAuditStore(t)
	page := NewPage("RE: 견적", "프로젝트", nil)
	page.Meta.ID = "m9"
	if err := store.WritePage(MailAnalysisPagePath("영산고", "m9"), page); err != nil {
		t.Fatal(err)
	}
	if got := store.FindMailAnalysisPage("m9"); got != MailAnalysisPagePath("영산고", "m9") {
		t.Fatalf("FindMailAnalysisPage = %q", got)
	}
	if got := store.FindMailAnalysisPage("없는놈"); got != "" {
		t.Fatalf("missing msgID must return \"\", got %q", got)
	}
	// A curated page whose filename merely matches is NOT a mail page.
	other := NewPage("m9", "기타", nil)
	if err := store.WritePage("기타/m9.md", other); err != nil {
		t.Fatal(err)
	}
	if got := store.FindMailAnalysisPage("m9"); got != MailAnalysisPagePath("영산고", "m9") {
		t.Fatalf("non-mail bucket page must not match: %q", got)
	}
}
