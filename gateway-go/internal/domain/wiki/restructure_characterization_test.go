package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func restructureActionIndex(t *testing.T, actions []string, fragment string) int {
	t.Helper()
	for i, action := range actions {
		if strings.Contains(action, fragment) {
			return i
		}
	}
	t.Fatalf("action containing %q not found in %v", fragment, actions)
	return -1
}

func TestRestructure_DryRunPreservesPhaseOrder(t *testing.T) {
	store := newRestructureStore(t)
	plan := []RestructureOp{
		{Op: "set-client", Source: "프로젝트/영산고.md", Target: "영산학원", Note: "거래처 분류"},
		{Op: "fold-log", Source: "프로젝트/영산고-계약-법무검토-(2026-06-30).md", Target: "영산고", Note: "사건 로그화"},
	}

	report, err := RestructureProjectLayout(store, plan, false)
	if err != nil {
		t.Fatal(err)
	}
	ordered := []int{
		restructureActionIndex(t, report.Actions, "client 프로젝트/영산고.md"),
		restructureActionIndex(t, report.Actions, "create 프로젝트/영산고/로그.md"),
		restructureActionIndex(t, report.Actions, "merge  프로젝트/영산고-계약-법무검토-(2026-06-30).md"),
		restructureActionIndex(t, report.Actions, "move   프로젝트/mail-analyses/19e8717314b5c915.md"),
		restructureActionIndex(t, report.Actions, "move   프로젝트/영산고.md → 프로젝트/영산고/대표.md"),
		restructureActionIndex(t, report.Actions, "create 프로젝트/기아-화성/대표.md"),
	}
	for i := 1; i < len(ordered); i++ {
		if ordered[i-1] >= ordered[i] {
			t.Fatalf("phase order indices = %v for actions %v", ordered, report.Actions)
		}
	}
}

func TestRestructure_DryRunPreservesMailCollisionPolicy(t *testing.T) {
	store := testutil.Must(NewStore(filepath.Join(t.TempDir(), "wiki"), ""))
	t.Cleanup(func() { store.Close() })

	write := func(relPath, title, body string, related []string) {
		t.Helper()
		page := NewPage(title, projectCategoryPrefix, nil)
		page.Body = body
		page.Meta.Related = related
		if err := store.WritePage(relPath, page); err != nil {
			t.Fatalf("WritePage(%s): %v", relPath, err)
		}
	}
	const repPath = "프로젝트/알파/대표.md"
	write(repPath, "알파", "대표 본문", nil)
	write("프로젝트/mail-analyses/aaaaaaaaaaaaaaaa.md", "동일 메일", "동일 본문", []string{repPath})
	write("프로젝트/알파/메일분석/aaaaaaaaaaaaaaaa.md", "동일 메일", "동일 본문", nil)
	write("프로젝트/mail-analyses/bbbbbbbbbbbbbbbb.md", "충돌 메일", "원본 본문", []string{repPath})
	write("프로젝트/알파/메일분석/bbbbbbbbbbbbbbbb.md", "충돌 메일", "다른 본문", nil)

	report, err := RestructureProjectLayout(store, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	restructureActionIndex(t, report.Actions, "delete 프로젝트/mail-analyses/aaaaaaaaaaaaaaaa.md")
	foundDifferentCollision := false
	for _, skipped := range report.Skipped {
		if strings.Contains(skipped, "mail-analysis collision (내용 상이)") &&
			strings.Contains(skipped, "bbbbbbbbbbbbbbbb") {
			foundDifferentCollision = true
		}
	}
	if !foundDifferentCollision {
		t.Fatalf("different-content collision missing from skips: %v", report.Skipped)
	}
	for _, source := range []string{
		"프로젝트/mail-analyses/aaaaaaaaaaaaaaaa.md",
		"프로젝트/mail-analyses/bbbbbbbbbbbbbbbb.md",
	} {
		if _, err := store.ReadPage(source); err != nil {
			t.Fatalf("dry run mutated %s: %v", source, err)
		}
	}
}

func TestRestructure_ApplyContinuesAfterActionFailure(t *testing.T) {
	store := testutil.Must(NewStore(filepath.Join(t.TempDir(), "wiki"), ""))
	t.Cleanup(func() { store.Close() })
	for _, relPath := range []string{"업무/실패.md", "업무/후속.md"} {
		page := NewPage(strings.TrimSuffix(filepath.Base(relPath), ".md"), "업무", nil)
		page.Body = "보존할 본문"
		if err := store.WritePage(relPath, page); err != nil {
			t.Fatal(err)
		}
	}
	// MovePage writes through <target>.tmp then renames it. A directory at the
	// final page path makes that rename fail without making the target a page in
	// the planning snapshot.
	if err := os.MkdirAll(filepath.Join(store.Dir(), "업무", "막힘.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	report, err := RestructureProjectLayout(store, []RestructureOp{
		{Op: "move", Source: "업무/실패.md", Target: "업무/막힘.md", Note: "실패 유도"},
		{Op: "delete", Source: "업무/후속.md", Note: "뒤 액션은 계속"},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Moved != 0 || report.Deleted != 1 || len(report.Errors) != 1 {
		t.Fatalf("partial result = %+v, want applied with 0 moves, 1 delete, 1 error", report)
	}
	if !strings.Contains(report.Errors[0], "move 업무/실패.md") {
		t.Fatalf("move error = %q", report.Errors[0])
	}
	if _, err := store.ReadPage("업무/실패.md"); err != nil {
		t.Fatalf("failed move lost its source: %v", err)
	}
	if _, err := store.ReadPage("업무/후속.md"); err == nil {
		t.Fatal("later delete did not run after the move failure")
	}
	if _, ok := store.snapshotEntries()["업무/실패.md"]; !ok {
		t.Fatal("final index rebuild omitted the surviving failed-move source")
	}
}
