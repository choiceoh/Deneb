package wiki

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// prunePastDate is the fixed Updated stamp the fixtures carry, so the "hygiene
// writes must not stamp Updated" assertions can actually fail (comparing
// against NewPage's today-stamp could not).
const prunePastDate = "2024-03-01"

// writePrunePage writes a fixture page with a fixed past Updated date.
func writePrunePage(t *testing.T, store *Store, path, title, id string, related []string) {
	t.Helper()
	page := NewPage(title, "프로젝트", nil)
	page.Meta.ID = id
	page.Meta.Related = related
	page.Meta.Updated = prunePastDate
	page.Body = "# " + title
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("WritePage(%s): %v", path, err)
	}
}

// TestPruneDeadRelatedLinksUpdatesRelated covers every repair tier and the
// drop path.
func TestPruneDeadRelatedLinksUpdatesRelated(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	defer store.Close()

	// Resolution targets — one page per repair tier so repairs don't collapse
	// into duplicates of each other.
	writePrunePage(t, store, "프로젝트/영산고/대표.md", "영산고", "yeongsan", nil)
	writePrunePage(t, store, "프로젝트/영산고/메일분석/19e8717314b5c914.md", "RE: 견적", "", nil)
	writePrunePage(t, store, "업무/구리값-동향.md", "구리값 동향", "", nil)
	writePrunePage(t, store, "업무/전선-시황.md", "전선 시황", "", nil)
	writePrunePage(t, store, "업무/알루미늄-시황.md", "알루미늄 시황", "al-market", nil)
	writePrunePage(t, store, "업무/케이블-시황.md", "케이블 시황", "", nil)

	// The rot-carrying page: one entry per repair tier + garbage + self.
	writePrunePage(t, store, "프로젝트/부산8호/대표.md", "부산8호", "", []string{
		"업무/구리값-동향",                         // missing .md → normalized
		"프로젝트/영산고.md",                       // legacy flat → 대표.md slot
		"mail-analyses/19e8717314b5c914.md", // category-less → unique basename
		"전선 시황",                             // title → path
		"al-market",                         // frontmatter ID → path
		`업무\케이블-시황.md`,                      // windows separators → normalized
		"기타/사라진-문서.md",                      // dead → dropped
		"프로젝트/부산8호/대표.md",                   // self → dropped
	})

	stats, err := store.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("PruneDeadRelatedLinks: %v", err)
	}
	if stats.PagesChanged != 1 {
		t.Fatalf("PagesChanged = %d, want 1 (stats: %+v)", stats.PagesChanged, stats)
	}
	if stats.Removed != 2 { // 사라진-문서 + self
		t.Errorf("Removed = %d, want 2", stats.Removed)
	}
	if stats.Repointed != 6 { // normalize, legacy-flat, basename, title, id, backslash
		t.Errorf("Repointed = %d, want 6", stats.Repointed)
	}
	if stats.Failed != 0 {
		t.Errorf("Failed = %d, want 0", stats.Failed)
	}

	got := testutil.Must(store.ReadPage("프로젝트/부산8호/대표.md"))
	want := map[string]bool{
		"업무/구리값-동향.md":                      true,
		"프로젝트/영산고/대표.md":                    true,
		"프로젝트/영산고/메일분석/19e8717314b5c914.md": true,
		"업무/전선-시황.md":                       true,
		"업무/알루미늄-시황.md":                     true,
		"업무/케이블-시황.md":                      true,
	}
	if len(got.Meta.Related) != len(want) {
		t.Fatalf("related after prune = %v, want exactly %d canonical targets", got.Meta.Related, len(want))
	}
	for _, r := range got.Meta.Related {
		if !want[r] {
			t.Errorf("unexpected related entry after prune: %q (all: %v)", r, got.Meta.Related)
		}
	}
	// Hygiene write must not stamp Updated (dormancy signal preserved).
	if got.Meta.Updated != prunePastDate {
		t.Errorf("Updated stamped by hygiene write: %q, want %q", got.Meta.Updated, prunePastDate)
	}
	// The repaired edges' TARGET pages gained backlinks — that maintenance must
	// not stamp their Updated either (it made dormant projects look active).
	tgt := testutil.Must(store.ReadPage("프로젝트/영산고/대표.md"))
	if tgt.Meta.Updated != prunePastDate {
		t.Errorf("backlink maintenance stamped target Updated: %q, want %q", tgt.Meta.Updated, prunePastDate)
	}

	// Idempotent: second sweep is a no-op.
	again, err := store.PruneDeadRelatedLinks()
	if err != nil || again.PagesChanged != 0 {
		t.Errorf("second sweep = %+v (err %v), want no-op", again, err)
	}
}

// TestPruneDeadRelatedLinks_AmbiguousStaysDeleted: a basename shared by many
// pages (대표.md) must never be "repaired" by guessing.
func TestPruneDeadRelatedLinks_AmbiguousStaysDeleted(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	defer store.Close()

	for _, name := range []string{"영산고", "부산8호"} {
		page := NewPage(name, "프로젝트", nil)
		page.Body = "# " + name
		if err := store.WritePage(RepPagePath(name), page); err != nil {
			t.Fatal(err)
		}
	}
	holder := NewPage("보유", "업무", nil)
	holder.Meta.Related = []string{"프로젝트/없는사업/대표.md"} // basename 대표.md is ambiguous
	holder.Body = "# 보유"
	if err := store.WritePage("업무/보유.md", holder); err != nil {
		t.Fatal(err)
	}

	stats, err := store.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.Removed != 1 || stats.Repointed != 0 {
		t.Errorf("stats = %+v, want the ambiguous ref dropped, never guessed", stats)
	}
	got := testutil.Must(store.ReadPage("업무/보유.md"))
	if len(got.Meta.Related) != 0 {
		t.Errorf("related = %v, want empty", got.Meta.Related)
	}
}

// TestPruneDeadRelatedLinks_ExtensionRepairPreservesMutualBacklink: repairing
// "기술/b" → "기술/b.md" is the SAME file. The raw-string backlink diff used to
// see it as remove("기술/b")+add("기술/b.md") and the remove leg deleted the
// mutual backlink the repair was keeping.
func TestPruneDeadRelatedLinks_ExtensionRepairPreservesMutualBacklink(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	defer store.Close()

	writePrunePage(t, store, "업무/케이블.md", "케이블", "", nil)
	// Writing this page auto-adds the reverse edge onto 업무/케이블.md.
	writePrunePage(t, store, "업무/전선.md", "전선", "", []string{"업무/케이블"}) // extensionless

	before := testutil.Must(store.ReadPage("업무/케이블.md"))
	if len(before.Meta.Related) != 1 || before.Meta.Related[0] != "업무/전선.md" {
		t.Fatalf("fixture backlink missing: %v", before.Meta.Related)
	}

	stats, err := store.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.Repointed != 1 {
		t.Errorf("Repointed = %d, want 1 (extension normalize)", stats.Repointed)
	}
	after := testutil.Must(store.ReadPage("업무/케이블.md"))
	if len(after.Meta.Related) != 1 || after.Meta.Related[0] != "업무/전선.md" {
		t.Errorf("mutual backlink deleted by normalization repair: %v", after.Meta.Related)
	}
}

// TestPruneDeadRelatedLinks_PreservesLiveProjectCodeRefs: code-form Related
// entries are first-class move-stable edges (graph_query/projectOwnedRefs
// resolve them) — the sweep must keep them while any project page carries the
// code, and still drop codes no page carries.
func TestPruneDeadRelatedLinks_PreservesLiveProjectCodeRefs(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	defer store.Close()

	rep := NewPage("기아화성", "프로젝트", nil)
	rep.Meta.Code = "pl3-kia-mod-001"
	rep.Meta.Updated = prunePastDate
	rep.Body = "# 기아화성"
	if err := store.WritePage("프로젝트/기아화성/대표.md", rep); err != nil {
		t.Fatal(err)
	}
	writePrunePage(t, store, "업무/모듈-시황.md", "모듈 시황", "", []string{
		"pl3-kia-mod-001",     // live code → preserved verbatim
		"[[pl3-kia-mod-001]]", // wikilink-wrapped live code → brackets repaired off
		"pl9-zzz-mod-001",     // no page carries this code → dropped
	})

	stats, err := store.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	got := testutil.Must(store.ReadPage("업무/모듈-시황.md"))
	if len(got.Meta.Related) != 1 || got.Meta.Related[0] != "pl3-kia-mod-001" {
		t.Fatalf("related = %v, want the live code ref preserved once", got.Meta.Related)
	}
	if stats.Removed != 2 { // dead code + bracket-form duplicate of the kept code
		t.Errorf("Removed = %d, want 2", stats.Removed)
	}
}

func TestPruneDeadLinksSkipGeneratedFactProjection(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.response_length", Value: "답변은 짧게 유지한다",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}
	if page, err := store.ReadPage(factProfilePagePath); err != nil || !isGeneratedFactProjectionPage(factProfilePagePath, page) {
		t.Fatalf("generated fact projection missing: page=%+v err=%v", page, err)
	}

	relatedStats, err := store.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("PruneDeadRelatedLinks: %v", err)
	}
	if relatedStats.Failed != 0 {
		t.Fatalf("PruneDeadRelatedLinks failed on generated fact projection: %+v", relatedStats)
	}
	bodyStats, _, err := store.PruneDeadWikiLinks()
	if err != nil {
		t.Fatalf("PruneDeadWikiLinks: %v", err)
	}
	if bodyStats.Failed != 0 {
		t.Fatalf("PruneDeadWikiLinks failed on generated fact projection: %+v", bodyStats)
	}
}

// TestPruneDeadRelatedLinks_SkipsOnEmptyIndex: with a lost/empty master index
// but pages on disk, the sweep must refuse to run — resolving against the
// empty index would deem every reference dead and strip Related wiki-wide.
func TestPruneDeadRelatedLinks_SkipsOnEmptyIndex(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	store := testutil.Must(NewStore(wikiDir, ""))
	writePrunePage(t, store, "업무/케이블.md", "케이블", "", nil)
	writePrunePage(t, store, "업무/전선.md", "전선", "", []string{"업무/케이블.md"})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate index.md loss: a fresh store builds an EMPTY index.
	if err := os.Remove(filepath.Join(wikiDir, "index.md")); err != nil {
		t.Fatal(err)
	}
	store2 := testutil.Must(NewStore(wikiDir, ""))
	defer store2.Close()

	stats, err := store2.PruneDeadRelatedLinks()
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.PagesChanged != 0 || stats.Removed != 0 {
		t.Fatalf("stats = %+v, want a skipped sweep", stats)
	}
	got := testutil.Must(store2.ReadPage("업무/전선.md"))
	if len(got.Meta.Related) != 1 || got.Meta.Related[0] != "업무/케이블.md" {
		t.Errorf("related stripped despite empty index: %v", got.Meta.Related)
	}
}
