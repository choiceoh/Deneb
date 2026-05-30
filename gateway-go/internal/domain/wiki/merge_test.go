package wiki

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// relatedSet collects a page's Related list into a set for order-independent
// membership assertions.
func relatedSet(t *testing.T, store *Store, relPath string) map[string]bool {
	t.Helper()
	page := testutil.Must(store.ReadPage(relPath))
	set := make(map[string]bool, len(page.Meta.Related))
	for _, r := range page.Meta.Related {
		set[r] = true
	}
	return set
}

// TestStore_MergePage_FullScenario exercises the whole merge: body replacement,
// frontmatter union, repointing a third page that referenced the source, and
// deletion of the source page.
func TestStore_MergePage_FullScenario(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	const (
		target = "프로젝트/topsolar.md"
		source = "프로젝트/topsolar-dup.md"
		kim    = "사람/kim.md"
		skt    = "거래/skt.md"
		policy = "결정/policy.md"
	)

	// Neighbor pages the projects link to.
	if err := store.WritePage(kim, NewPage("Kim", "사람", nil)); err != nil {
		t.Fatalf("WritePage(kim): %v", err)
	}
	if err := store.WritePage(skt, NewPage("SKT deal", "거래", nil)); err != nil {
		t.Fatalf("WritePage(skt): %v", err)
	}

	// Target: tags [erp], importance 0.5, links to kim.
	tgt := NewPage("TopSolar", "프로젝트", []string{"erp"})
	tgt.Meta.Importance = 0.5
	tgt.Meta.Related = []string{kim}
	tgt.Body = "# TopSolar\n\n태양광 시공 ERP."
	if err := store.WritePage(target, tgt); err != nil {
		t.Fatalf("WritePage(target): %v", err)
	}

	// Source: tags [solar, erp], importance 0.8, due set, links to skt.
	src := NewPage("TopSolar 중복", "프로젝트", []string{"solar", "erp"})
	src.Meta.Importance = 0.8
	src.Meta.Due = "2026-06-01"
	src.Meta.Related = []string{skt}
	src.Body = "# 중복 프로젝트\n\n같은 태양광 ERP 프로젝트."
	if err := store.WritePage(source, src); err != nil {
		t.Fatalf("WritePage(source): %v", err)
	}

	// A third page that references the SOURCE directly (inbound link). This is
	// the link that must be repointed to the target on merge.
	pol := NewPage("정책", "결정", nil)
	pol.Meta.Related = []string{source}
	if err := store.WritePage(policy, pol); err != nil {
		t.Fatalf("WritePage(policy): %v", err)
	}

	// Merge source → target with an explicit synthesized body.
	const mergedBody = "# TopSolar (통합)\n\n태양광 시공 ERP. 중복 제거 완료."
	res, err := store.MergePage(target, source, mergedBody, MergeOptions{})
	if err != nil {
		t.Fatalf("MergePage: %v", err)
	}

	// Result summary.
	if res.TargetPath != target {
		t.Errorf("res.TargetPath = %q, want %q", res.TargetPath, target)
	}
	if !res.SourceRemoved {
		t.Error("res.SourceRemoved = false, want true")
	}
	if res.MergedTitle != "TopSolar" {
		t.Errorf("res.MergedTitle = %q, want TopSolar (target title kept)", res.MergedTitle)
	}
	// skt (source's neighbor, repointed) + policy (inbound, repointed) = 2.
	if res.RewriteCount != 2 {
		t.Errorf("res.RewriteCount = %d, want 2", res.RewriteCount)
	}

	// (a) Body replaced with the synthesized text.
	got := testutil.Must(store.ReadPage(target))
	if got.Body != mergedBody {
		t.Errorf("target body = %q, want %q", got.Body, mergedBody)
	}

	// (b) Frontmatter union: tags = erp ∪ solar, importance = max, due carried.
	tagSet := map[string]bool{}
	for _, tg := range got.Meta.Tags {
		tagSet[tg] = true
	}
	if !tagSet["erp"] || !tagSet["solar"] {
		t.Errorf("target tags = %v, want union containing erp+solar", got.Meta.Tags)
	}
	if got.Meta.Importance != 0.8 {
		t.Errorf("target importance = %v, want 0.8 (max)", got.Meta.Importance)
	}
	if got.Meta.Due != "2026-06-01" {
		t.Errorf("target due = %q, want 2026-06-01 (carried from source)", got.Meta.Due)
	}

	// (c) Source's neighbor (skt) and the inbound third page (policy) now point
	//     at the target, and the target links back to all three neighbors.
	tgtRelated := relatedSet(t, store, target)
	for _, want := range []string{kim, skt, policy} {
		if !tgtRelated[want] {
			t.Errorf("target.Related missing %q: %v", want, tgtRelated)
		}
	}
	if policySet := relatedSet(t, store, policy); !policySet[target] || policySet[source] {
		t.Errorf("policy.Related = %v, want repointed to target, no source", policySet)
	}
	if sktSet := relatedSet(t, store, skt); !sktSet[target] || sktSet[source] {
		t.Errorf("skt.Related = %v, want repointed to target, no source", sktSet)
	}

	// (d) No self-reference and no dangling reference to the deleted source.
	if tgtRelated[target] {
		t.Errorf("target.Related references itself: %v", tgtRelated)
	}
	if tgtRelated[source] {
		t.Errorf("target.Related references the deleted source: %v", tgtRelated)
	}

	// (e) Source page is gone from disk.
	if _, err := store.ReadPage(source); err == nil {
		t.Error("source page still readable after merge, want deleted")
	}
}

// TestStore_MergePage_EmptyBodyFallsBackToConcat verifies that an empty
// mergedBody never wipes content — the source body is concatenated in.
func TestStore_MergePage_EmptyBodyFallsBackToConcat(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	tgt := NewPage("A", "프로젝트", nil)
	tgt.Body = "내용 A"
	_ = store.WritePage("프로젝트/a.md", tgt)

	src := NewPage("B", "프로젝트", nil)
	src.Body = "내용 B"
	_ = store.WritePage("프로젝트/b.md", src)

	if _, err := store.MergePage("프로젝트/a.md", "프로젝트/b.md", "", MergeOptions{}); err != nil {
		t.Fatalf("MergePage: %v", err)
	}

	got := testutil.Must(store.ReadPage("프로젝트/a.md"))
	if !strings.Contains(got.Body, "내용 A") || !strings.Contains(got.Body, "내용 B") {
		t.Errorf("merged body = %q, want both source and target content", got.Body)
	}
}

// TestStore_MergePage_RejectsSelfMerge guards against merging a page into
// itself, which would otherwise read-then-delete the same file.
func TestStore_MergePage_RejectsSelfMerge(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	_ = store.WritePage("프로젝트/a.md", NewPage("A", "프로젝트", nil))
	if _, err := store.MergePage("프로젝트/a.md", "프로젝트/a.md", "x", MergeOptions{}); err == nil {
		t.Error("MergePage(self, self) = nil error, want rejection")
	}
}
