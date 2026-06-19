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

func TestNotebookUnknownAction(t *testing.T) {
	fn, _ := newTestNotebookTool(t)
	out := callNotebook(t, fn, map[string]any{"action": "frobnicate"})
	if !strings.Contains(out, "알 수 없는 액션") {
		t.Fatalf("expected unknown-action notice, got %q", out)
	}
}
