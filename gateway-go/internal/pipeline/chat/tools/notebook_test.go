package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func newTestNotebookTool(t *testing.T) (toolctx.ToolFunc, *toolctx.NotebookDeps) {
	t.Helper()
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("notebook.NewStore: %v", err)
	}
	deps := &toolctx.NotebookDeps{Store: store, Wiki: newTestWikiStore(t)}
	return ToolNotebook(deps), deps
}

func callNotebook(t *testing.T, fn toolctx.ToolFunc, payload map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(payload)
	out, err := fn(context.Background(), raw)
	if err != nil {
		t.Fatalf("notebook tool error: %v", err)
	}
	return out
}

// extractID pulls the "id=<x>" token a create/add response embeds.
func extractID(t *testing.T, deps *toolctx.NotebookDeps) string {
	t.Helper()
	list := deps.Store.List()
	if len(list) == 0 {
		t.Fatal("no notebook created")
	}
	return list[0].ID
}

func TestNotebookDisabledWhenNoStore(t *testing.T) {
	fn := ToolNotebook(&toolctx.NotebookDeps{})
	out := callNotebook(t, fn, map[string]any{"action": "list"})
	if !strings.Contains(out, "비활성") {
		t.Fatalf("expected disabled notice, got %q", out)
	}
}

func TestNotebookBriefGroundsAndCites(t *testing.T) {
	fn, deps := newTestNotebookTool(t)

	callNotebook(t, fn, map[string]any{
		"action": "create", "name": "탑솔라 딜", "description": "공급 계약",
	})
	id := extractID(t, deps)

	// One note source, one wiki source (page pre-loaded by newTestWikiStore).
	callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "note",
		"text": "견적가 1억 2천만원, 납기 8주.",
	})
	callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "wiki",
		"ref": "phase-2-summary.md",
	})

	brief := callNotebook(t, fn, map[string]any{"action": "brief", "id": id, "focus": "계약 조건"})

	var parsed struct {
		Notebook    map[string]string `json:"notebook"`
		Focus       string            `json:"focus"`
		Instruction string            `json:"instruction"`
		Sources     []struct {
			Cite string `json:"cite"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(brief), &parsed); err != nil {
		t.Fatalf("brief is not valid JSON: %v\n%s", err, brief)
	}
	if len(parsed.Sources) != 2 {
		t.Fatalf("sources = %d, want 2", len(parsed.Sources))
	}
	if parsed.Sources[0].Cite != "S1" || parsed.Sources[1].Cite != "S2" {
		t.Fatalf("cites = %q,%q want S1,S2", parsed.Sources[0].Cite, parsed.Sources[1].Cite)
	}
	if !strings.Contains(parsed.Sources[0].Text, "견적가") {
		t.Fatalf("note source text not carried: %q", parsed.Sources[0].Text)
	}
	if strings.TrimSpace(parsed.Sources[1].Text) == "" {
		t.Fatal("wiki source text was not read into the brief")
	}
	if !strings.Contains(parsed.Instruction, "[S1]") {
		t.Fatalf("instruction should tell the model to cite inline: %q", parsed.Instruction)
	}
	if parsed.Focus != "계약 조건" {
		t.Fatalf("focus = %q, want 계약 조건", parsed.Focus)
	}
}

func TestNotebookBriefEmpty(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "빈 노트북"})
	id := extractID(t, deps)
	out := callNotebook(t, fn, map[string]any{"action": "brief", "id": id})
	if !strings.Contains(out, "자료가 없어") {
		t.Fatalf("empty brief should explain there are no sources: %q", out)
	}
}

func TestNotebookWikiSourceMissingPageNoted(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "nb"})
	id := extractID(t, deps)
	callNotebook(t, fn, map[string]any{
		"action": "add_source", "id": id, "kind": "wiki", "ref": "does/not-exist.md",
	})
	brief := callNotebook(t, fn, map[string]any{"action": "brief", "id": id})
	if !strings.Contains(brief, "읽기 실패") {
		t.Fatalf("missing wiki page should produce a read-failure note: %q", brief)
	}
}

func TestNotebookBriefRespectsByteBudget(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "큰 노트북"})
	id := extractID(t, deps)

	// Several large Korean note sources (3 bytes/rune) — naive per-source caps
	// would blow the 24KB byte-enforced tool budget and corrupt the JSON.
	big := strings.Repeat("가나다라마", 4000) // 20k runes ≈ 60KB each
	for i := 0; i < 6; i++ {
		callNotebook(t, fn, map[string]any{
			"action": "add_source", "id": id, "kind": "note", "text": big,
		})
	}

	brief := callNotebook(t, fn, map[string]any{"action": "brief", "id": id})
	if len(brief) > 24000 {
		t.Fatalf("brief is %d bytes, exceeds the 24KB tool budget", len(brief))
	}
	var parsed struct {
		Sources []struct {
			Note string `json:"note"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(brief), &parsed); err != nil {
		t.Fatalf("over-budget brief produced invalid JSON: %v", err)
	}
	if len(parsed.Sources) != 6 {
		t.Fatalf("sources = %d, want 6", len(parsed.Sources))
	}
	if !strings.Contains(parsed.Sources[0].Note, "잘림") {
		t.Fatalf("truncated source should carry a truncation note: %q", parsed.Sources[0].Note)
	}
}

func TestNotebookBriefManySourcesStaysValidJSON(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	callNotebook(t, fn, map[string]any{"action": "create", "name": "다건 노트북"})
	id := extractID(t, deps)

	// Many sources: even minimal per-source text + JSON envelope must not push
	// the encoded brief past the cap into head/tail-truncated invalid JSON.
	mid := strings.Repeat("내용", 600) // ~3.6KB each
	for i := 0; i < 60; i++ {
		callNotebook(t, fn, map[string]any{
			"action": "add_source", "id": id, "kind": "note", "text": mid,
		})
	}
	brief := callNotebook(t, fn, map[string]any{"action": "brief", "id": id})
	if len(brief) > 24000 {
		t.Fatalf("brief is %d bytes, exceeds the 24KB tool budget", len(brief))
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(brief), &parsed); err != nil {
		t.Fatalf("many-source brief produced invalid JSON: %v", err)
	}
	if _, ok := parsed["sources"]; !ok {
		t.Fatal("brief missing sources field")
	}
}

func TestNotebookPinToDealAndResolveByDealRef(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	const deal = "프로젝트/탑솔라.md"

	// pin_to_deal auto-creates the deal's notebook and pins in one shot.
	out := callNotebook(t, fn, map[string]any{
		"action": "pin_to_deal", "deal_ref": deal, "deal_name": "탑솔라 딜",
		"kind": "note", "text": "견적가 1.2억",
	})
	if !strings.Contains(out, "S1") {
		t.Fatalf("pin_to_deal should report the pinned cite: %q", out)
	}
	nb, ok := deps.Store.GetByDealRef(deal)
	if !ok || len(nb.Sources) != 1 {
		t.Fatalf("deal notebook not created/pinned: %+v ok=%v", nb, ok)
	}

	// A second pin reuses the same notebook (no duplicate).
	callNotebook(t, fn, map[string]any{
		"action": "pin_to_deal", "deal_ref": deal, "kind": "note", "text": "납기 8주",
	})
	if got := deps.Store.List(); len(got) != 1 {
		t.Fatalf("pin_to_deal created a duplicate notebook: %d", len(got))
	}

	// brief resolves by deal_ref (no id needed) and grounds on both sources.
	brief := callNotebook(t, fn, map[string]any{"action": "brief", "deal_ref": deal})
	var parsed struct {
		Sources []struct {
			Text string `json:"text"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(brief), &parsed); err != nil {
		t.Fatalf("brief by deal_ref not JSON: %v\n%s", err, brief)
	}
	if len(parsed.Sources) != 2 {
		t.Fatalf("brief by deal_ref sources = %d, want 2", len(parsed.Sources))
	}
}

func TestNotebookForDealCreatesAndShows(t *testing.T) {
	fn, deps := newTestNotebookTool(t)
	out := callNotebook(t, fn, map[string]any{
		"action": "for_deal", "deal_ref": "프로젝트/x.md", "deal_name": "X 딜",
	})
	if !strings.Contains(out, "X 딜") {
		t.Fatalf("for_deal should show the deal notebook: %q", out)
	}
	if _, ok := deps.Store.GetByDealRef("프로젝트/x.md"); !ok {
		t.Fatal("for_deal should have created the deal notebook")
	}
}

func TestNotebookPinToDealRequiresRef(t *testing.T) {
	fn, _ := newTestNotebookTool(t)
	out := callNotebook(t, fn, map[string]any{"action": "pin_to_deal", "kind": "note", "text": "x"})
	if !strings.Contains(out, "deal_ref") {
		t.Fatalf("pin_to_deal without deal_ref should prompt for it: %q", out)
	}
}

func TestNotebookUnknownAction(t *testing.T) {
	fn, _ := newTestNotebookTool(t)
	out := callNotebook(t, fn, map[string]any{"action": "frobnicate"})
	if !strings.Contains(out, "알 수 없는 액션") {
		t.Fatalf("expected unknown-action notice, got %q", out)
	}
}
