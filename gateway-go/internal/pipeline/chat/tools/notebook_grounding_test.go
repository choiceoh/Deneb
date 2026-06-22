package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func TestBuildNotebookGroundingSoftAndStrict(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "탑솔라 딜"})
	id := extractID(t, deps)
	callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "note", "text": "견적가 1.2억, 납기 8주.",
	})
	callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "wiki", "ref": "phase-2-summary.md",
	})

	// Soft (default): sources lead with [S#] cites, and the directive permits
	// marked supplementation "(자료 밖)".
	g, ok := BuildNotebookGrounding(deps, id)
	if !ok {
		t.Fatal("grounding should be built for a notebook with sources")
	}
	for _, want := range []string{"탑솔라 딜", "[S1]", "[S2]", "견적가", "1차 근거", "(자료 밖)"} {
		if !strings.Contains(g, want) {
			t.Fatalf("soft grounding missing %q:\n%s", want, g)
		}
	}

	// Strict: the directive forbids outside knowledge and drops the "(자료 밖)" path.
	if err := deps.Store.SetMode(id, "strict"); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	g, ok = BuildNotebookGrounding(deps, id)
	if !ok || !strings.Contains(g, "자료에만 근거") || strings.Contains(g, "(자료 밖)") {
		t.Fatalf("strict grounding wrong:\n%s", g)
	}
}

func TestBuildNotebookGroundingEmptyOrMissing(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	// Missing notebook → nothing to inject.
	if _, ok := BuildNotebookGrounding(deps, "does-not-exist"); ok {
		t.Fatal("missing notebook should yield ok=false")
	}
	// Created but sourceless → nothing to inject.
	callNotebook(t, fn, map[string]any{"action": "create", "name": "빈"})
	id := extractID(t, deps)
	if _, ok := BuildNotebookGrounding(deps, id); ok {
		t.Fatal("empty notebook should yield ok=false")
	}
	// Nil deps / blank id are safe no-ops.
	if _, ok := BuildNotebookGrounding(nil, id); ok {
		t.Fatal("nil deps should yield ok=false")
	}
	if _, ok := BuildNotebookGrounding(deps, ""); ok {
		t.Fatal("blank id should yield ok=false")
	}
}

func TestBuildNotebookGroundingByteBudget(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "큰 노트북"})
	id := extractID(t, deps)
	big := strings.Repeat("가나다라마", 4000) // 20k runes ≈ 60KB each
	for i := 0; i < 6; i++ {
		callNotebook(t, fn, map[string]any{"action": "add_source", "id": id, "kind": "note", "text": big})
	}
	g, ok := BuildNotebookGrounding(deps, id)
	if !ok {
		t.Fatal("grounding should build for a large notebook")
	}
	if len(g) > notebookGroundingMaxBytes {
		t.Fatalf("grounding is %d bytes, exceeds the %d budget", len(g), notebookGroundingMaxBytes)
	}
}

func TestNotebookOpenCloseBindsSession(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "탑솔라"})
	id := extractID(t, deps)
	callNotebook(t, fn, map[string]any{"action": "add_source", "id": id, "kind": "note", "text": "견적가 1.2억"})

	const sk = "client:test-notebook-open-close"
	toolctx.ClearActiveNotebook(sk) // isolate from any prior test (package-global store)
	ctx := toolctx.WithSessionKey(context.Background(), sk)

	raw, _ := json.Marshal(map[string]any{"action": "open", "id": id})
	out, err := fn(ctx, raw)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.Contains(out, "열림") {
		t.Fatalf("open should confirm: %q", out)
	}
	if got := toolctx.ActiveNotebook(sk); got != id {
		t.Fatalf("after open, ActiveNotebook = %q, want %q", got, id)
	}

	raw, _ = json.Marshal(map[string]any{"action": "close"})
	if _, err := fn(ctx, raw); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := toolctx.ActiveNotebook(sk); got != "" {
		t.Fatalf("after close, ActiveNotebook = %q, want empty", got)
	}
}

func TestNotebookModeAction(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "계약 검토"})
	id := extractID(t, deps)

	out := callNotebook(t, fn, map[string]any{"action": "mode", "id": id, "mode": "strict"})
	if !strings.Contains(out, "strict") {
		t.Fatalf("mode action should confirm strict: %q", out)
	}
	if nb, _ := deps.Store.Get(id); nb == nil || nb.Mode != "strict" {
		t.Fatalf("notebook mode not persisted strict: %+v", nb)
	}
	// "soft" normalizes back to the empty (default) mode.
	callNotebook(t, fn, map[string]any{"action": "mode", "id": id, "mode": "soft"})
	if nb, _ := deps.Store.Get(id); nb == nil || nb.Mode != "" {
		t.Fatalf("soft should normalize to empty mode: %+v", nb)
	}
}

func TestNotebookAddFileSourceText(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "문서 노트북"})
	id := extractID(t, deps)

	// A plain-text file is read directly (no OCR backend needed), snapshotted
	// into the source, and flows into the grounding like a note.
	path := filepath.Join(t.TempDir(), "spec.txt")
	if err := os.WriteFile(path, []byte("계약 단가는 1억 2천만원이고 납기는 8주다."), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	out := callNotebook(t, fn, map[string]any{"action": "add_source", "id": id, "kind": "file", "ref": path})
	if !strings.Contains(out, "S1") {
		t.Fatalf("add file source should report a cite: %q", out)
	}
	g, ok := BuildNotebookGrounding(deps, id)
	if !ok || !strings.Contains(g, "계약 단가") || !strings.Contains(g, "spec.txt") {
		t.Fatalf("file content/title not grounded:\n%s", g)
	}
}

func TestNotebookAddFileSourceMissing(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "nb"})
	id := extractID(t, deps)
	out := callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "file", "ref": "/no/such/file.pdf",
	})
	if !strings.Contains(out, "실패") || !strings.Contains(out, "찾을 수 없") {
		t.Fatalf("missing file should produce a graceful failure: %q", out)
	}
	if nb, _ := deps.Store.Get(id); nb != nil && len(nb.Sources) != 0 {
		t.Fatalf("failed file ingest must not pin a source: %d", len(nb.Sources))
	}
}

func TestNotebookExternalSourceIngest(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	// Inject a fake reader for the external kinds (the pointer is the one
	// ToolNotebook captured, so setting fields after construction is seen).
	fake := func(_ context.Context, ref string) (string, error) { return "원격 본문: " + ref, nil }
	deps.FetchURL, deps.ReadMail, deps.ReadDiary = fake, fake, fake

	callNotebook(t, fn, map[string]any{"action": "create", "name": "외부소스"})
	id := extractID(t, deps)
	for _, tc := range []struct{ kind, ref string }{
		{"url", "https://example.com/spec"},
		{"mail", "thread-123"},
		{"diary", "2026-06-22"},
	} {
		out := callNotebook(t, fn, map[string]any{"action": "add_source", "id": id, "kind": tc.kind, "ref": tc.ref})
		if !strings.Contains(out, "핀 완료") {
			t.Fatalf("%s ingest should pin a source: %q", tc.kind, out)
		}
	}
	g, ok := BuildNotebookGrounding(deps, id)
	if !ok || !strings.Contains(g, "example.com/spec") || !strings.Contains(g, "thread-123") || !strings.Contains(g, "2026-06-22") {
		t.Fatalf("external sources not grounded:\n%s", g)
	}
}

func TestNotebookExternalSourceUnwired(t *testing.T) {
	fn, deps := newTestNotebookTool(t) // no readers wired
	callNotebook(t, fn, map[string]any{"action": "create", "name": "nb"})
	id := extractID(t, deps)
	out := callNotebook(t, fn, map[string]any{"action": "add_source", "id": id, "kind": "url", "ref": "https://x"})
	if !strings.Contains(out, "비활성") {
		t.Fatalf("unwired url reader should report disabled: %q", out)
	}
	if nb, _ := deps.Store.Get(id); nb != nil && len(nb.Sources) != 0 {
		t.Fatalf("unwired ingest must not pin: %d", len(nb.Sources))
	}
}

func TestNotebookOpenDedicatedGuard(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "탑솔라"})
	idA := extractID(t, deps)
	callNotebook(t, fn, map[string]any{"action": "add_source", "id": idA, "kind": "note", "text": "a"})
	callNotebook(t, fn, map[string]any{"action": "create", "name": "와트라디"})
	var idB string
	for _, nb := range deps.Store.List() {
		if nb.ID != idA {
			idB = nb.ID
		}
	}
	callNotebook(t, fn, map[string]any{"action": "add_source", "id": idB, "kind": "note", "text": "b"})

	// From a dedicated session for idA, opening a DIFFERENT notebook (idB) must
	// be refused — not reported as a false success (the key-derived id wins).
	ctx := toolctx.WithSessionKey(context.Background(), toolctx.NotebookSessionPrefix+idA)
	raw, _ := json.Marshal(map[string]any{"action": "open", "id": idB})
	out, err := fn(ctx, raw)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !strings.Contains(out, "전용 세션") {
		t.Fatalf("opening a different notebook from a dedicated session should be refused: %q", out)
	}
	if got := toolctx.ActiveNotebook(toolctx.NotebookSessionPrefix + idA); got != idA {
		t.Fatalf("dedicated session must stay on its own notebook, got %q", got)
	}
}
