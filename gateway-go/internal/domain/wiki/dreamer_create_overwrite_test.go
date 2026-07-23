package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestWikiDreamer_CreateOnExistingPathConvertsToUpdateNotOverwrite guards the
// data-loss path where the dreamer emits action:"create" for a path that already
// exists exactly. findExistingPage/FindSimilarPages seed seen{self:true}, so the
// exact target is never returned as a "similar" match — before the fix the create
// fell through to WritePage and replaced the existing page body wholesale with the
// one-line synthesis. The create must instead be converted to an update that
// merges into the existing body.
func TestWikiDreamer_CreateOnExistingPathConvertsToUpdateNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, logger)

	const path = "사람/홍길동.md"
	existing := NewPage("홍길동", "사람", nil)
	existing.Body = "# 홍길동\n\n## 핵심 사실\n- 대표이사\n- 계약 2건 진행 중\n"
	if err := store.WritePage(path, existing); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	// LLM proposes a *create* at the exact existing path with a one-line body.
	u := wikiUpdate{Action: "create", Path: path, Title: "홍길동", Content: "### 2026-07-15 신규 사실 한 줄"}
	u = wd.retargetDreamUpdate(u)

	if u.Action != "update" {
		t.Fatalf("create on an existing path was not converted to update: action=%q", u.Action)
	}

	// Persisting must MERGE into the existing body, not overwrite it.
	out := wd.persistDreamUpdate(u, "", "")
	if out.failed {
		t.Fatal("persistDreamUpdate reported failure")
	}
	got := testutil.Must(store.ReadPage(path))
	if !strings.Contains(got.Body, "대표이사") {
		t.Errorf("existing body was overwritten (lost prior '대표이사' fact): %q", got.Body)
	}
	if !strings.Contains(got.Body, "신규 사실 한 줄") {
		t.Errorf("new synthesized content was not merged in: %q", got.Body)
	}
}

// TestWikiDreamer_UpdatePersistsConfirmedSitesAndKinds guards that the update
// path persists 현장(sites)/특성(kinds) — they are usually confirmed after the
// rep page already exists, and the update path previously dropped them (only
// create set them), so "현장이 확인되면 기입/갱신" never took effect.
func TestWikiDreamer_UpdatePersistsConfirmedSitesAndKinds(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, logger)

	const path = "프로젝트/군산/대표.md"
	existing := NewPage("군산", "프로젝트", nil)
	existing.Body = "# 군산\n\n## 현재 상태\n진행 중."
	if err := store.WritePage(path, existing); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	u := wikiUpdate{
		Action: "update", Path: path, Title: "군산", Content: "현장 확인",
		Sites: flexStringList{"전북 군산시 옥구읍 수산리"},
		Kinds: flexStringList{"태양광"},
	}
	if out := wd.persistDreamUpdate(u, "", ""); out.failed {
		t.Fatal("persistDreamUpdate reported failure")
	}
	got := testutil.Must(store.ReadPage(path))
	if !hasStr(got.Meta.Sites, "전북 군산시 옥구읍 수산리") {
		t.Errorf("confirmed site was dropped on update: %v", got.Meta.Sites)
	}
	if !hasStr(got.Meta.Kinds, "태양광") {
		t.Errorf("confirmed kind was dropped on update: %v", got.Meta.Kinds)
	}
}
