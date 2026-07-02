package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// newRestructureStore builds a fixture wiki mirroring the production mess:
// legacy flat 대표페이지, mail-ID pages scattered inside project folders and the
// legacy mail-analyses bucket, and a topic-duplicate pair.
func newRestructureStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })

	write := func(path, title, body string, related []string) {
		t.Helper()
		page := NewPage(title, "프로젝트", nil)
		page.Meta.Related = related
		page.Body = body
		if err := store.WritePage(path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", path, err)
		}
	}

	// Legacy flat 대표페이지 (heavily referenced by mail pages).
	write("프로젝트/영산고.md", "영산고", "# 영산고\n본문", nil)
	// Duplicate topic page to be merged by the plan.
	write("프로젝트/영산고-태양광.md", "영산고 태양광", "# 영산고 태양광\n중복 본문", nil)
	// Mail-ID page inside the project folder (analyzer-written, old scheme).
	write("프로젝트/영산고/19e8717314b5c914.md", "RE: 견적", "> From: kim@x.com\n> Message ID: `19e8717314b5c914`\n\n분석",
		[]string{"프로젝트/영산고.md"})
	// Mail page in the legacy global bucket with a related project.
	write("프로젝트/mail-analyses/19e8717314b5c915.md", "FW: 계약", "> From: lee@x.com\n> Message ID: `19e8717314b5c915`\n\n분석",
		[]string{"프로젝트/영산고.md"})
	// Mail page with no project linkage → unlinked bucket.
	write("프로젝트/mail-analyses/19e8717314b5c916.md", "뉴스레터", "> From: news@x.com\n> Message ID: `19e8717314b5c916`\n\n분석", nil)
	// Event page the plan folds into the project log.
	write("프로젝트/영산고-계약-법무검토-(2026-06-30).md", "영산고 계약 법무검토", "검토 내용", nil)
	// Deal ledger page must stay put.
	write("프로젝트/거래/한빛전기.md", "한빛전기", "거래 이력", nil)
	// Folder-only project with no 대표페이지 (the production norm pre-migration).
	write("프로젝트/기아-화성/치장장-태양광-배치.md", "기아 화성 치장장", "배치 검토", nil)

	return store
}

func TestRestructure_DryRunWritesNothing(t *testing.T) {
	store := newRestructureStore(t)
	before := testutil.Must(store.ListPages(""))

	rep, err := RestructureProjectLayout(store, nil, false)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if rep.Applied {
		t.Fatal("dry-run must not report Applied")
	}
	if len(rep.Actions) == 0 {
		t.Fatal("dry-run should propose actions for the messy fixture")
	}
	after := testutil.Must(store.ListPages(""))
	if len(before) != len(after) {
		t.Fatalf("dry-run mutated the store: %d → %d pages", len(before), len(after))
	}
}

func TestRestructure_Apply(t *testing.T) {
	store := newRestructureStore(t)

	plan := []RestructureOp{
		{Op: "merge", Source: "프로젝트/영산고-태양광.md", Target: "프로젝트/영산고.md", Note: "같은 프로젝트 중복"},
		{Op: "fold-log", Source: "프로젝트/영산고-계약-법무검토-(2026-06-30).md", Target: "영산고", Note: "사건 → 로그"},
	}
	rep, err := RestructureProjectLayout(store, plan, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !rep.Applied || len(rep.Errors) > 0 {
		t.Fatalf("apply failed: %+v", rep)
	}

	// Flat 대표페이지 moved into the folder slot with the merged duplicate.
	repPage, err := store.ReadPage("프로젝트/영산고/대표.md")
	if err != nil {
		t.Fatalf("대표.md missing after apply: %v", err)
	}
	if !strings.Contains(repPage.Body, "중복 본문") || !strings.Contains(repPage.Body, "## 병합: 영산고 태양광") {
		t.Errorf("merged duplicate body missing from 대표.md:\n%s", repPage.Body)
	}
	if _, err := store.ReadPage("프로젝트/영산고.md"); err == nil {
		t.Error("legacy flat page should be gone")
	}
	if _, err := store.ReadPage("프로젝트/영산고-태양광.md"); err == nil {
		t.Error("merged duplicate should be gone")
	}

	// Event page folded into 로그.md as a dated section.
	logPage, err := store.ReadPage("프로젝트/영산고/로그.md")
	if err != nil {
		t.Fatalf("로그.md missing after fold-log: %v", err)
	}
	if !strings.Contains(logPage.Body, "영산고 계약 법무검토") || !strings.Contains(logPage.Body, "검토 내용") {
		t.Errorf("folded event missing from 로그.md:\n%s", logPage.Body)
	}

	// Mail pages landed in their 메일분석 slots.
	for _, want := range []string{
		"프로젝트/영산고/메일분석/19e8717314b5c914.md",
		"프로젝트/영산고/메일분석/19e8717314b5c915.md",
		"프로젝트/메일분석/19e8717314b5c916.md",
	} {
		if _, err := store.ReadPage(want); err != nil {
			t.Errorf("expected mail page at %s: %v", want, err)
		}
	}
	// Their old locations are gone.
	for _, gone := range []string{
		"프로젝트/영산고/19e8717314b5c914.md",
		"프로젝트/mail-analyses/19e8717314b5c915.md",
		"프로젝트/mail-analyses/19e8717314b5c916.md",
	} {
		if _, err := store.ReadPage(gone); err == nil {
			t.Errorf("stale mail page still present at %s", gone)
		}
	}

	// The moved mail page's Related repointed to the new 대표페이지 path.
	mail, err := store.ReadPage("프로젝트/영산고/메일분석/19e8717314b5c914.md")
	if err != nil {
		t.Fatalf("read moved mail page: %v", err)
	}
	found := false
	for _, r := range mail.Meta.Related {
		if r == "프로젝트/영산고/대표.md" {
			found = true
		}
		if r == "프로젝트/영산고.md" {
			t.Errorf("stale related ref survived the move: %v", mail.Meta.Related)
		}
	}
	if !found {
		t.Errorf("related not repointed to the new 대표페이지: %v", mail.Meta.Related)
	}

	// Deal ledger untouched.
	if _, err := store.ReadPage("프로젝트/거래/한빛전기.md"); err != nil {
		t.Errorf("deal ledger page must stay put: %v", err)
	}

	// Folder-only project got a minted 대표페이지 titled by the project.
	kiaRep, err := store.ReadPage("프로젝트/기아-화성/대표.md")
	if err != nil {
		t.Fatalf("folder-only project must gain a 대표페이지: %v", err)
	}
	if kiaRep.Meta.Title != "기아-화성" || kiaRep.Meta.Type != "project" {
		t.Errorf("minted rep page = title %q type %q", kiaRep.Meta.Title, kiaRep.Meta.Type)
	}

	// The emptied legacy mail-analyses directory is pruned from disk.
	if _, statErr := os.Stat(filepath.Join(store.Dir(), "프로젝트", "mail-analyses")); !os.IsNotExist(statErr) {
		t.Errorf("legacy mail-analyses dir should be pruned, stat err = %v", statErr)
	}

	// Idempotent: a second apply finds nothing left to do.
	rep2, err := RestructureProjectLayout(store, nil, true)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(rep2.Actions) != 0 {
		t.Errorf("second apply should be a no-op, got %v", rep2.Actions)
	}
}
