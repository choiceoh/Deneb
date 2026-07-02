package wiki

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestPruneDeadRelatedLinks covers every repair tier and the drop path.
func TestPruneDeadRelatedLinks(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	defer store.Close()

	write := func(path, title, id string, related []string) {
		t.Helper()
		page := NewPage(title, "프로젝트", nil)
		page.Meta.ID = id
		page.Meta.Related = related
		page.Body = "# " + title
		if err := store.WritePage(path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", path, err)
		}
	}

	// Resolution targets — one page per repair tier so repairs don't collapse
	// into duplicates of each other.
	write("프로젝트/영산고/대표.md", "영산고", "yeongsan", nil)
	write("프로젝트/영산고/메일분석/19e8717314b5c914.md", "RE: 견적", "", nil)
	write("업무/구리값-동향.md", "구리값 동향", "", nil)
	write("업무/전선-시황.md", "전선 시황", "", nil)
	write("업무/알루미늄-시황.md", "알루미늄 시황", "al-market", nil)

	// The rot-carrying page: one entry per repair tier + garbage + self.
	write("프로젝트/부산8호/대표.md", "부산8호", "", []string{
		"업무/구리값-동향",                         // missing .md → normalized
		"프로젝트/영산고.md",                       // legacy flat → 대표.md slot
		"mail-analyses/19e8717314b5c914.md", // category-less → unique basename
		"전선 시황",                             // title → path
		"al-market",                         // frontmatter ID → path
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
	if stats.Repointed != 5 { // normalize, legacy-flat, basename, title, id
		t.Errorf("Repointed = %d, want 5", stats.Repointed)
	}

	got := testutil.Must(store.ReadPage("프로젝트/부산8호/대표.md"))
	want := map[string]bool{
		"업무/구리값-동향.md":                      true,
		"프로젝트/영산고/대표.md":                    true,
		"프로젝트/영산고/메일분석/19e8717314b5c914.md": true,
		"업무/전선-시황.md":                       true,
		"업무/알루미늄-시황.md":                     true,
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
	if got.Meta.Updated != NewPage("x", "프로젝트", nil).Meta.Updated {
		t.Errorf("Updated stamped by hygiene write: %q", got.Meta.Updated)
	}

	// Idempotent: second sweep is a no-op.
	again, err := store.PruneDeadRelatedLinks()
	if err != nil || again.PagesChanged != 0 {
		t.Errorf("second sweep = %+v (err %v), want no-op", again, err)
	}
}

// TestPruneDeadRelatedLinks_AmbiguousStaysDropped: a basename shared by many
// pages (대표.md) must never be "repaired" by guessing.
func TestPruneDeadRelatedLinks_AmbiguousStaysDropped(t *testing.T) {
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
