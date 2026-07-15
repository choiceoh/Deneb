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
	out := wd.persistDreamUpdate(u, "")
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
