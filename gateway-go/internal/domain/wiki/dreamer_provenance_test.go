package wiki

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplyUpdatesStampsAndAppendsEpisodeProvenance verifies the capture side of
// the "citation needed" loop: a create stamps the page with its source episode,
// and a later update from a different episode appends (not replaces) it, so a
// merged fact stays traceable to every diary span that shaped it.
func TestApplyUpdatesStampsAndAppendsEpisodeProvenance(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := NewWikiDreamer(store, nil, "", Config{Enabled: true}, slog.Default())

	const path = "업무/구리-가격.md"
	firstEpisode := newEpisodeRef("2026-07-08", "구리 현물가 관련 일지 원본")
	if _, _, _, _, _ = wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "create", Path: path, Title: "구리 가격", Category: "업무",
		Content: "구리 현물가 메모.",
	}}, firstEpisode); firstEpisode == "" {
		t.Fatal("first episode ref should be non-empty")
	}

	page, err := store.ReadPage(path)
	if err != nil || page == nil {
		t.Fatalf("page missing after create: %v", err)
	}
	if len(page.Meta.Sources) != 1 || page.Meta.Sources[0] != firstEpisode {
		t.Fatalf("create did not stamp provenance: %v (want [%s])", page.Meta.Sources, firstEpisode)
	}

	secondEpisode := newEpisodeRef("2026-07-20", "구리 가격 갱신 일지 원본")
	wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "update", Path: path, Title: "구리 가격", Category: "업무",
		Content: "## 갱신\n현물가 상향.",
	}}, secondEpisode)

	page, err = store.ReadPage(path)
	if err != nil || page == nil {
		t.Fatalf("page missing after update: %v", err)
	}
	if len(page.Meta.Sources) != 2 {
		t.Fatalf("update did not append provenance, sources=%v", page.Meta.Sources)
	}
	if page.Meta.Sources[0] != firstEpisode || page.Meta.Sources[1] != secondEpisode {
		t.Fatalf("provenance chain wrong: %v", page.Meta.Sources)
	}

	// Re-applying the SAME episode must not duplicate the ref (idempotent).
	wd.applyUpdates(context.Background(), []wikiUpdate{{
		Action: "update", Path: path, Title: "구리 가격", Category: "업무",
		Content: "## 갱신\n현물가 상향.",
	}}, secondEpisode)
	page, _ = store.ReadPage(path)
	if len(page.Meta.Sources) != 2 {
		t.Fatalf("re-applying same episode duplicated provenance: %v", page.Meta.Sources)
	}
}

// TestMergeFrontmatterUnionsSources guards that a duplicate-fold keeps both
// pages' provenance: the source page is deleted after its body folds in, so
// dropping its refs would leave the merged facts uncitable in the graph.
func TestMergeFrontmatterUnionsSources(t *testing.T) {
	dst := Frontmatter{Sources: []string{"d2026-07-01#aaaa"}}
	src := Frontmatter{Sources: []string{"d2026-07-10#bbbb", "d2026-07-01#aaaa"}}
	mergeFrontmatterInto(&dst, src)
	if len(dst.Sources) != 2 {
		t.Fatalf("union dropped/duplicated refs: %v", dst.Sources)
	}
	if dst.Sources[0] != "d2026-07-01#aaaa" || dst.Sources[1] != "d2026-07-10#bbbb" {
		t.Fatalf("unexpected union: %v", dst.Sources)
	}
}

// TestSplitPageChildrenInheritSources guards that when an oversized page is
// split, the child pages (which carry the actual facts) inherit the parent's
// episode provenance instead of being written with empty sources.
func TestSplitPageChildrenInheritSources(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	big := NewPage("Big Page", "업무", nil)
	big.Meta.Sources = []string{"d2026-07-15#dddd"}
	filler := strings.Repeat("가", 400)
	big.Body = "## 섹션 하나\n" + filler + "\n\n## 섹션 둘\n" + filler + "\n\n## 섹션 셋\n" + filler
	if err := store.WritePage("업무/big.md", big); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	children, err := store.splitPage("업무/big.md", 500)
	if err != nil {
		t.Fatalf("splitPage: %v", err)
	}
	if len(children) < 2 {
		t.Fatalf("expected a real split, got children=%v", children)
	}
	for _, c := range children {
		child, err := store.ReadPage(c)
		if err != nil {
			t.Fatalf("ReadPage(%s): %v", c, err)
		}
		if len(child.Meta.Sources) != 1 || child.Meta.Sources[0] != "d2026-07-15#dddd" {
			t.Errorf("child %s did not inherit provenance: %v", c, child.Meta.Sources)
		}
	}
}

// TestSeedPersonPagesStampsProvenance guards the auxiliary person-seed write
// path: a seeded person page must trace back to the diary span whose repeated
// mentions triggered the seed.
func TestSeedPersonPagesStampsProvenance(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	wd := &WikiDreamer{store: store, logger: slog.Default()}
	wd.SetPersonDirectory(func() []PersonSeed {
		return []PersonSeed{{Name: "김민준", Org: "현대차 구매팀"}}
	})

	input := "김민준 부장과 통화. 견적은 김민준 부장이 검토 후 회신."
	ref := "d2026-07-18#eeee"
	if created := wd.seedPersonPages(context.Background(), input, ref); created != 1 {
		t.Fatalf("want 1 seeded page, got %d", created)
	}
	page, err := store.ReadPage("인물/김민준.md")
	if err != nil {
		t.Fatalf("seeded page missing: %v", err)
	}
	if len(page.Meta.Sources) != 1 || page.Meta.Sources[0] != ref {
		t.Errorf("seed did not stamp provenance: %v", page.Meta.Sources)
	}
}
