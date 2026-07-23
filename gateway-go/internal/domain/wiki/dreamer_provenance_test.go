package wiki

import (
	"context"
	"log/slog"
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
