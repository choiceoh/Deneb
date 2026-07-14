// store_hardening_test.go — regression tests for the 2026-07 STORE-CORE audit
// fixes: index snapshot reads (no live-map walkers), self-merge guards, Related
// persistence in index.md, frontmatter escaping, single-critical-section
// FoldDuplicate, boot orphan adoption, disk-based link pruning, backlink
// normalization, and the external path guard. Each test failed (or raced)
// against the pre-fix store.
package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// --- Finding 1: Store.Index() exposed the live mutable index -----------------

// TestStore_ConcurrentWriteAndIndexWalkers_Race is the committed form of the
// audit probe that crashed the pre-fix store with "concurrent map iteration
// and map write": page writers mutate the index map in place under s.mu while
// a dozen callers walked the live map lock-free (Tier1Pages released the lock
// before iterating; FindSimilarPages/verify/RPC walked Store.Index().Entries).
// All read surfaces now run on deep-copied snapshots. Run with -race.
func TestStore_ConcurrentWriteAndIndexWalkers_Race(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	for i := 0; i < 20; i++ {
		p := NewPage(fmt.Sprintf("Seed %d", i), "프로젝트", []string{"tag"})
		p.Meta.Importance = 0.9
		p.Body = "# seed\n\nbody"
		if err := store.WritePage(fmt.Sprintf("프로젝트/seed-%d/대표.md", i), p); err != nil {
			t.Fatalf("seed WritePage: %v", err)
		}
	}

	const iters = 120
	var wg sync.WaitGroup

	// Writer: every WritePage rewrites an index entry (Tags/Related included)
	// in place under s.mu.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			p := NewPage(fmt.Sprintf("W %d", i), "프로젝트", []string{"t", fmt.Sprintf("t%d", i)})
			p.Meta.Importance = 0.9
			p.Meta.Related = []string{"프로젝트/seed-0/대표.md"}
			p.Body = "# w\n\nbody"
			if err := store.WritePage(fmt.Sprintf("프로젝트/new-%d/대표.md", i%10), p); err != nil {
				t.Errorf("WritePage: %v", err)
				return
			}
		}
	}()

	// The formerly lock-free walkers, one goroutine each.
	walkers := []func(){
		func() { _ = store.Tier1Pages(0.5) },
		func() {
			_ = store.FindSimilarPages(context.Background(),
				SimilarQuery{ID: "x", Title: "Seed", Category: "프로젝트"}, 3)
		},
		func() { _ = store.SnapshotIndex().Render() },
		func() {
			for range store.snapshotEntries() {
			}
		},
		func() { _ = store.FlagDormantProjects(time.Now(), 1) },
	}
	for _, walk := range walkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				walk()
			}
		}()
	}
	wg.Wait()
}

// TestSnapshotEntries_DeepCopy: mutating a snapshot (map or slice fields) must
// not leak into the live index.
func TestSnapshotEntries_DeepCopy(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	p := NewPage("스냅샷", "기타", []string{"태그1"})
	p.Meta.Related = []string{"기타/이웃.md"}
	p.Body = "# s"
	if err := store.WritePage("기타/스냅샷.md", p); err != nil {
		t.Fatal(err)
	}

	snap := store.snapshotEntries()
	e := snap["기타/스냅샷.md"]
	if len(e.Tags) != 1 || len(e.Related) != 1 {
		t.Fatalf("snapshot entry incomplete: %+v", e)
	}
	e.Tags[0] = "오염"
	e.Related[0] = "오염"
	delete(snap, "기타/스냅샷.md")

	fresh := store.snapshotEntries()["기타/스냅샷.md"]
	if fresh.Tags[0] != "태그1" || fresh.Related[0] != "기타/이웃.md" {
		t.Errorf("snapshot mutation leaked into live index: %+v", fresh)
	}
}

// --- Finding 2: self-merge via spelling variant deleted the page -------------

// TestStore_MergePage_RejectsSelfMergeSpellingVariant: "기타/dup" and
// "기타/dup.md" are the same file; the pre-fix raw-string guard let the pair
// through and the "merge" deleted the page it had just written.
func TestStore_MergePage_RejectsSelfMergeSpellingVariant(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	p := NewPage("Dup", "기타", nil)
	p.Body = "# Dup\n\nprecious content"
	if err := store.WritePage("기타/dup.md", p); err != nil {
		t.Fatal(err)
	}

	if _, err := store.MergePage("기타/dup", "기타/dup.md", "", MergeOptions{}); err == nil {
		t.Error("MergePage accepted a self-merge spelling variant")
	}
	got, err := store.ReadPage("기타/dup.md")
	if err != nil {
		t.Fatalf("page destroyed by rejected self-merge: %v", err)
	}
	if !strings.Contains(got.Body, "precious content") {
		t.Errorf("page content damaged: %q", got.Body)
	}
}

// TestStore_FoldDuplicate_RejectsSelfFold: FoldDuplicate had NO self-merge
// guard at all — even the exact same spelling folded a page into itself and
// deleted it.
func TestStore_FoldDuplicate_RejectsSelfFold(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	p := NewPage("Dup2", "기타", nil)
	p.Body = "# Dup2\n\nprecious content"
	if err := store.WritePage("기타/dup2.md", p); err != nil {
		t.Fatal(err)
	}

	for _, fold := range []string{"기타/dup2.md", "기타/dup2"} {
		if err := store.FoldDuplicate("기타/dup2", fold); err == nil {
			t.Errorf("FoldDuplicate(기타/dup2, %q) accepted a self-fold", fold)
		}
	}
	if _, err := store.ReadPage("기타/dup2.md"); err != nil {
		t.Fatalf("page destroyed by rejected self-fold: %v", err)
	}
}

// --- Finding 3: IndexEntry.Related not persisted in index.md -----------------

// TestIndex_RelatedPersistsAcrossReload: the related column round-trips
// through Render/parseIndex, including items that carry the inner separator.
func TestIndex_RelatedPersistsAcrossReload(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "index.md")

	idx := newIndex()
	idx.updateEntry("프로젝트/영산고/대표.md", &Page{Meta: Frontmatter{
		Title:    "영산고",
		Category: "프로젝트",
		Related:  []string{"인물/김민준.md", "프로젝트/영산고/로그.md", "pipe|in|item"},
		Updated:  "2026-07-01",
	}})
	if err := idx.Save(idxPath); err != nil {
		t.Fatal(err)
	}

	got := testutil.Must(parseIndex(idxPath)).Entries["프로젝트/영산고/대표.md"]
	want := []string{"인물/김민준.md", "프로젝트/영산고/로그.md", "pipe in item"}
	if len(got.Related) != len(want) {
		t.Fatalf("related = %v, want %v", got.Related, want)
	}
	for i := range want {
		if got.Related[i] != want[i] {
			t.Errorf("related[%d] = %q, want %q", i, got.Related[i], want[i])
		}
	}
}

// TestIndex_EmptyIDRowRoundtrip: an entry with no frontmatter ID renders as
// "\tpath\t…"; the parser must not TrimSpace the leading tab away, which
// shifted every field one column left (path read as ID, title as path) and
// corrupted the whole row on reload. Surfaced while persisting Related.
func TestIndex_EmptyIDRowRoundtrip(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, "index.md")

	idx := newIndex()
	idx.updateEntry("기타/무제.md", &Page{Meta: Frontmatter{
		Title:   "무제 페이지", // no ID
		Related: []string{"기타/이웃.md"},
		Updated: "2026-07-01",
	}})
	if err := idx.Save(idxPath); err != nil {
		t.Fatal(err)
	}
	got, ok := testutil.Must(parseIndex(idxPath)).Entries["기타/무제.md"]
	if !ok {
		t.Fatal("ID-less entry lost its path key on reload (field shift)")
	}
	if got.Title != "무제 페이지" || got.Updated != "2026-07-01" {
		t.Errorf("fields shifted: %+v", got)
	}
	if len(got.Related) != 1 || got.Related[0] != "기타/이웃.md" {
		t.Errorf("related = %v", got.Related)
	}
}

// TestParseTSVLine_OldFormatWithoutRelated: pre-related-column lines still
// parse; Related simply stays nil.
func TestParseTSVLine_OldFormatWithoutRelated(t *testing.T) {
	// 11-field format (through created, no related).
	line := "id1\t기타/a.md\t제목\t요약\ttag1,tag2\t0.70\t2026-07-01\tconcept\thigh\t2\t2026-06-01"
	e := parseTSVLine(line, "기타")
	if e.path != "기타/a.md" || e.entry.Created != "2026-06-01" {
		t.Fatalf("old-format parse broken: %+v", e)
	}
	if e.entry.Related != nil {
		t.Errorf("Related should stay nil for old-format lines, got %v", e.entry.Related)
	}
}

// TestStore_RelatedSurvivesRestart_BacklinkRemoval is the end-to-end probe:
// pre-fix, a restart wiped in-memory Related (index.md didn't carry it), so
// the FIRST post-restart write diffed against oldRelated=nil and left a stale
// reverse edge on the formerly-related page.
func TestStore_RelatedSurvivesRestart_BacklinkRemoval(t *testing.T) {
	dir := t.TempDir()
	wikiDir, diaryDir := filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")

	store := testutil.Must(NewStore(wikiDir, diaryDir))
	y := NewPage("Y", "기타", nil)
	y.Body = "# Y"
	if err := store.WritePage("기타/y.md", y); err != nil {
		t.Fatal(err)
	}
	x := NewPage("X", "기타", nil)
	x.Meta.Related = []string{"기타/y.md"}
	x.Body = "# X"
	if err := store.WritePage("기타/x.md", x); err != nil {
		t.Fatal(err)
	}
	// Backlink maintenance made the edge mutual.
	if got := testutil.Must(store.ReadPage("기타/y.md")); len(got.Meta.Related) != 1 {
		t.Fatalf("backlink not established: %v", got.Meta.Related)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: the reloaded index must still know X→Y.
	store2 := testutil.Must(NewStore(wikiDir, diaryDir))
	defer store2.Close()
	if rel := store2.snapshotEntries()["기타/x.md"].Related; len(rel) != 1 || rel[0] != "기타/y.md" {
		t.Fatalf("Related lost across restart: %v", rel)
	}

	// First post-restart write drops the X→Y edge; Y's reverse edge must go too.
	x2 := NewPage("X", "기타", nil)
	x2.Body = "# X v2"
	if err := store2.WritePage("기타/x.md", x2); err != nil {
		t.Fatal(err)
	}
	if got := testutil.Must(store2.ReadPage("기타/y.md")); len(got.Meta.Related) != 0 {
		t.Errorf("stale reverse edge survived restart+rewrite: %v", got.Meta.Related)
	}
}

// --- Finding 4: unescaped scalar frontmatter values ---------------------------

// TestPage_RenderNewlineScalarsKeepMetadata: a newline in an LLM-supplied
// title/summary used to terminate the frontmatter line early and shred every
// following field into the body.
func TestPage_RenderNewlineScalarsKeepMetadata(t *testing.T) {
	p := &Page{
		Meta: Frontmatter{
			Title:      "제목 첫 줄\n주입된 둘째 줄",
			Summary:    "요약\r\n다음 줄",
			Category:   "기타",
			Tags:       []string{"태그"},
			Importance: 0.7,
			Type:       "concept",
		},
		Body: "본문입니다.",
	}
	got := testutil.Must(parsePage(p.Render()))
	if got.Meta.Title != "제목 첫 줄 주입된 둘째 줄" {
		t.Errorf("title = %q", got.Meta.Title)
	}
	if got.Meta.Summary != "요약 다음 줄" {
		t.Errorf("summary = %q", got.Meta.Summary)
	}
	// The metadata after the newline-bearing fields must survive intact.
	if got.Meta.Category != "기타" || len(got.Meta.Tags) != 1 ||
		got.Meta.Importance != 0.7 || got.Meta.Type != "concept" {
		t.Errorf("metadata shredded: %+v", got.Meta)
	}
	if strings.Contains(got.Body, "category:") || strings.Contains(got.Body, "tags:") {
		t.Errorf("frontmatter leaked into body: %q", got.Body)
	}
	if !strings.Contains(got.Body, "본문입니다.") {
		t.Errorf("body lost: %q", got.Body)
	}
}

// --- Finding 5: commas inside flow-array items split on reparse ---------------

// TestPage_RenderFlowArrayCommaRoundtrip: a cue like "계약금, 선수금 일정" must
// stay ONE item across a render/parse cycle (the comma becomes "·" — documented
// lossy-but-stable substitution).
func TestPage_RenderFlowArrayCommaRoundtrip(t *testing.T) {
	p := &Page{
		Meta: Frontmatter{
			Title: "쉼표",
			Cues:  []string{"계약금, 선수금 일정", "정상 큐"},
			Tags:  []string{"a,b", "c"},
		},
		Body: "b",
	}
	got := testutil.Must(parsePage(p.Render()))
	if len(got.Meta.Cues) != 2 {
		t.Fatalf("cues split: %v", got.Meta.Cues)
	}
	if got.Meta.Cues[0] != "계약금· 선수금 일정" || got.Meta.Cues[1] != "정상 큐" {
		t.Errorf("cues = %v", got.Meta.Cues)
	}
	if len(got.Meta.Tags) != 2 || got.Meta.Tags[0] != "a·b" {
		t.Errorf("tags = %v", got.Meta.Tags)
	}
	// Idempotent: a second cycle must not change anything further.
	again := testutil.Must(parsePage(got.Render()))
	if len(again.Meta.Cues) != 2 || again.Meta.Cues[0] != got.Meta.Cues[0] {
		t.Errorf("second roundtrip drifted: %v", again.Meta.Cues)
	}
}

// --- Finding 6: FoldDuplicate weaker than MergePage ---------------------------

// TestStore_FoldDuplicate_MergesLikeMergePage: the automated fold now runs the
// same locked merge as MergePage — inbound references repointed, cues/code
// inherited, body preserved under the marker.
func TestStore_FoldDuplicate_MergesLikeMergePage(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	keep := NewPage("탑솔라", "프로젝트", []string{"태양광"})
	keep.Body = "# 탑솔라\n\n본문 유지"
	if err := store.WritePage("프로젝트/탑솔라/대표.md", keep); err != nil {
		t.Fatal(err)
	}
	fold := NewPage("탑솔라 (중복)", "프로젝트", []string{"중복태그"})
	fold.Meta.Code = "pl3-top-mod-001"
	fold.Meta.Cues = []string{"선수금 일정"}
	fold.Body = "# 중복\n\n중복 페이지의 본문"
	if err := store.WritePage("프로젝트/탑솔라-중복/대표.md", fold); err != nil {
		t.Fatal(err)
	}
	// Third page referencing the fold — must be repointed to keep.
	ref := NewPage("참조자", "기타", nil)
	ref.Meta.Related = []string{"프로젝트/탑솔라-중복/대표.md"}
	ref.Body = "# ref"
	if err := store.WritePage("기타/참조자.md", ref); err != nil {
		t.Fatal(err)
	}

	if err := store.FoldDuplicate("프로젝트/탑솔라/대표.md", "프로젝트/탑솔라-중복/대표.md"); err != nil {
		t.Fatalf("FoldDuplicate: %v", err)
	}

	got := testutil.Must(store.ReadPage("프로젝트/탑솔라/대표.md"))
	if !strings.Contains(got.Body, "병합된 중복 문서") || !strings.Contains(got.Body, "중복 페이지의 본문") {
		t.Errorf("folded body lost: %q", got.Body)
	}
	if got.Meta.Code != "pl3-top-mod-001" {
		t.Errorf("frozen code not inherited: %q", got.Meta.Code)
	}
	if len(got.Meta.Cues) == 0 || got.Meta.Cues[0] != "선수금 일정" {
		t.Errorf("cues not inherited: %v", got.Meta.Cues)
	}
	refGot := testutil.Must(store.ReadPage("기타/참조자.md"))
	if len(refGot.Meta.Related) != 1 || refGot.Meta.Related[0] != "프로젝트/탑솔라/대표.md" {
		t.Errorf("inbound reference not repointed: %v", refGot.Meta.Related)
	}
	if _, err := store.ReadPage("프로젝트/탑솔라-중복/대표.md"); !os.IsNotExist(err) {
		t.Errorf("fold page should be deleted, err=%v", err)
	}
}

// TestStore_FoldDuplicate_SerializesUnderWriteMu: the pre-fix implementation
// read the fold page OUTSIDE the write lock and took three separate critical
// sections, so a write landing on fold mid-fold was silently destroyed. The
// fold must now block behind writeMu for its whole duration (same structural
// guard as TestStore_RebuildIndex_SerializesAgainstWriters).
func TestStore_FoldDuplicate_SerializesUnderWriteMu(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	for _, p := range []string{"기타/keep.md", "기타/fold.md"} {
		page := NewPage(strings.TrimSuffix(filepath.Base(p), ".md"), "기타", nil)
		page.Body = "# " + p
		if err := store.WritePage(p, page); err != nil {
			t.Fatal(err)
		}
	}

	store.writeMu.Lock()
	done := make(chan error, 1)
	go func() { done <- store.FoldDuplicate("기타/keep.md", "기타/fold.md") }()

	select {
	case <-done:
		store.writeMu.Unlock()
		t.Fatal("FoldDuplicate completed while writeMu was held; it must run inside the write lock")
	case <-time.After(200 * time.Millisecond):
		// Expected: blocked on writeMu.
	}
	store.writeMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("FoldDuplicate: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FoldDuplicate did not complete after writeMu was released")
	}
}

// --- Finding 7: boot consistency didn't adopt orphan pages ---------------------

// TestNewStore_AdoptsOrphanPages: a page on disk with no index entry (crash
// between page write and index save) must be adopted into the master index at
// startup, not stay invisible until the next dream-cycle rebuild.
func TestNewStore_AdoptsOrphanPages(t *testing.T) {
	dir := t.TempDir()
	wikiDir, diaryDir := filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")

	store := testutil.Must(NewStore(wikiDir, diaryDir))
	for _, name := range []string{"orphan", "kept"} {
		p := NewPage(name, "기타", nil)
		p.Body = "# " + name
		if err := store.WritePage("기타/"+name+".md", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate the crash window: strip the orphan's line from index.md.
	idxPath := filepath.Join(wikiDir, "index.md")
	data := string(testutil.Must(os.ReadFile(idxPath)))
	var lines []string
	for _, ln := range strings.Split(data, "\n") {
		if strings.Contains(ln, "기타/orphan.md") {
			continue
		}
		lines = append(lines, ln)
	}
	if err := os.WriteFile(idxPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	store2 := testutil.Must(NewStore(wikiDir, diaryDir))
	defer store2.Close()
	entries := store2.snapshotEntries()
	if e, ok := entries["기타/orphan.md"]; !ok || e.Title != "orphan" {
		t.Errorf("orphan page not adopted at startup: %+v", entries)
	}
	if _, ok := entries["기타/kept.md"]; !ok {
		t.Errorf("untouched entry lost: %+v", entries)
	}
}

// --- Finding 8: link pruning resolved existence against the (stale) index ------

// TestPruneDeadRelatedLinks_KeepsPagesMissingFromStaleIndex: a real on-disk
// page lagging from the index (the finding-7 crash window) must NOT have its
// inbound references pruned as dead — existence now resolves against disk.
func TestPruneDeadRelatedLinks_KeepsPagesMissingFromStaleIndex(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	b := NewPage("B", "기타", nil)
	b.Body = "# B"
	if err := store.WritePage("기타/b.md", b); err != nil {
		t.Fatal(err)
	}
	a := NewPage("A", "기타", nil)
	a.Meta.Related = []string{"기타/b.md"}
	a.Body = "# A"
	if err := store.WritePage("기타/a.md", a); err != nil {
		t.Fatal(err)
	}

	// Simulate a stale index: B's entry vanishes while the file stays on disk.
	store.mu.Lock()
	delete(store.index.Entries, "기타/b.md")
	store.mu.Unlock()

	if _, err := store.PruneDeadRelatedLinks(); err != nil {
		t.Fatalf("PruneDeadRelatedLinks: %v", err)
	}
	got := testutil.Must(store.ReadPage("기타/a.md"))
	found := false
	for _, r := range got.Meta.Related {
		if r == "기타/b.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("live page pruned as dead due to stale index: related=%v", got.Meta.Related)
	}
}

// --- Finding 9: UpdatePage treated read errors as "absent" ---------------------

// TestStore_UpdatePage_PropagatesReadError: a transient I/O failure (here:
// permissions) must fail the update, not masquerade as "page absent" and let
// the create branch overwrite the page.
func TestStore_UpdatePage_PropagatesReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits don't block reads")
	}
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	p := NewPage("보호", "기타", nil)
	p.Body = "# 보호\n\n지켜야 할 내용"
	if err := store.WritePage("기타/보호.md", p); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(store.Dir(), "기타", "보호.md")
	if err := os.Chmod(abs, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(abs, 0o644) //nolint:errcheck // restore for TempDir cleanup

	mutateCalled := false
	err := store.UpdatePage("기타/보호.md", func(cur *Page) (*Page, error) {
		mutateCalled = true
		return NewPage("덮어쓰기", "기타", nil), nil
	})
	if err == nil {
		t.Fatal("UpdatePage swallowed the read error and ran the create path")
	}
	if mutateCalled {
		t.Error("mutate ran despite an unreadable page (would overwrite content)")
	}
	if err := os.Chmod(abs, 0o644); err != nil {
		t.Fatal(err)
	}
	got := testutil.Must(store.ReadPage("기타/보호.md"))
	if !strings.Contains(got.Body, "지켜야 할 내용") {
		t.Errorf("page overwritten: %q", got.Body)
	}
}

// --- Finding 10: backlink presence/removal compared raw strings ----------------

// TestStore_Backlinks_NormalizedComparison: a related entry recorded without
// the ".md" extension is the same edge — the presence check must not stack a
// second spelling, and removal must clear the denormalized one.
func TestStore_Backlinks_NormalizedComparison(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// T carries a DENORMALIZED ref to a not-yet-existing src (so no backlink
	// maintenance rewrites it at write time).
	tp := NewPage("T", "기타", nil)
	tp.Meta.Related = []string{"기타/src"}
	tp.Body = "# T"
	if err := store.WritePage("기타/t.md", tp); err != nil {
		t.Fatal(err)
	}

	// src appears, related to T → addBacklink(T, "기타/src.md") must recognize
	// the existing "기타/src" spelling and NOT append a duplicate.
	src := NewPage("src", "기타", nil)
	src.Meta.Related = []string{"기타/t.md"}
	src.Body = "# src"
	if err := store.WritePage("기타/src.md", src); err != nil {
		t.Fatal(err)
	}
	got := testutil.Must(store.ReadPage("기타/t.md"))
	if len(got.Meta.Related) != 1 {
		t.Fatalf("duplicate normalized/denormalized backlink pair: %v", got.Meta.Related)
	}

	// Deleting src must remove the denormalized spelling too.
	if err := store.DeletePage("기타/src.md"); err != nil {
		t.Fatal(err)
	}
	got = testutil.Must(store.ReadPage("기타/t.md"))
	if len(got.Meta.Related) != 0 {
		t.Errorf("stale denormalized reverse edge survived deletion: %v", got.Meta.Related)
	}
}

// --- Finding 11: mergeFrontmatterInto dropped src.Code/Resource ----------------

func TestMergeFrontmatterInto_InheritsCodeAndResource(t *testing.T) {
	dst := Frontmatter{Title: "keep"}
	src := Frontmatter{Title: "fold", Code: "pl3-kia-mod-001", Resource: "gmail:thread-1"}
	mergeFrontmatterInto(&dst, src)
	if dst.Code != "pl3-kia-mod-001" {
		t.Errorf("empty dst.Code must inherit src's, got %q", dst.Code)
	}
	if dst.Resource != "gmail:thread-1" {
		t.Errorf("empty dst.Resource must inherit src's, got %q", dst.Resource)
	}

	// A non-empty dst keeps its own identity.
	dst2 := Frontmatter{Title: "keep", Code: "pl3-own-mod-002", Resource: "deal:7"}
	mergeFrontmatterInto(&dst2, src)
	if dst2.Code != "pl3-own-mod-002" || dst2.Resource != "deal:7" {
		t.Errorf("non-empty dst fields overwritten: %+v", dst2)
	}
}

// --- Finding 12: SplitByH2 treated fenced "## " lines as boundaries ------------

func TestPage_SplitByH2_IgnoresFencedHeadings(t *testing.T) {
	p := &Page{Body: strings.Join([]string{
		"preamble",
		"## 2026-07-01",
		"entry with a fence:",
		"```",
		"## not a heading",
		"code line",
		"```",
		"tail of same entry",
		"## 2026-07-02",
		"second entry",
	}, "\n")}

	preamble, sections := p.SplitByH2()
	if preamble != "preamble" {
		t.Errorf("preamble = %q", preamble)
	}
	if len(sections) != 2 {
		t.Fatalf("sections = %d, want 2 (fenced ## must not split); got %+v", len(sections), sections)
	}
	if sections[0].Heading != "2026-07-01" || sections[1].Heading != "2026-07-02" {
		t.Errorf("headings = %q / %q", sections[0].Heading, sections[1].Heading)
	}
	if !strings.Contains(sections[0].Content, "## not a heading") ||
		!strings.Contains(sections[0].Content, "tail of same entry") {
		t.Errorf("fenced content shredded: %q", sections[0].Content)
	}
}

// --- Finding 13: external path guard --------------------------------------------

func TestValidateExternalPath(t *testing.T) {
	valid := []string{
		"기타/a.md",
		"기타/a",
		"프로젝트/영산고/대표.md",
		"a/./b.md", // cleans inside the root
	}
	for _, p := range valid {
		if err := ValidateExternalPath(p); err != nil {
			t.Errorf("ValidateExternalPath(%q) = %v, want nil", p, err)
		}
	}
	invalid := []string{
		"",
		"..",
		"../secret.md",
		"기타/../../secret.md",
		"/etc/passwd",
		"\\\\server\\share",
		"기타\\a.md",
		"C:\\windows\\system32",
		"c:boot.ini",
	}
	for _, p := range invalid {
		if err := ValidateExternalPath(p); err == nil {
			t.Errorf("ValidateExternalPath(%q) = nil, want error", p)
		}
	}
}
