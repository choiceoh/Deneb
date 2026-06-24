package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func notebookTestMethods(t *testing.T) map[string]rpcutil.HandlerFunc {
	t.Helper()
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NotebookMethods(NotebookDeps{Store: func() (*notebook.Store, error) { return store, nil }})
}

func callNotebook(t *testing.T, m map[string]rpcutil.HandlerFunc, method string, params any) *protocol.ResponseFrame {
	t.Helper()
	h, ok := m[method]
	if !ok {
		t.Fatalf("no handler registered for %s", method)
	}
	req, err := protocol.NewRequestFrame("test-1", method, params)
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return h(clientauth.WithContext(context.Background(), sampleIdentity()), req)
}

// TestNotebookWriteFlow exercises the create → add_source (note + wiki) → get
// round-trip the desktop notebook pane drives.
func TestNotebookWriteFlow(t *testing.T) {
	m := notebookTestMethods(t)

	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "신규 딜"}))
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}
	if created["name"] != "신규 딜" {
		t.Errorf("create name = %v, want 신규 딜", created["name"])
	}

	// note source — explicit kind + text.
	note := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_source",
		map[string]any{"id": id, "kind": "note", "title": "잔금", "text": "최종 5% 잔금."}))
	if note["kind"] != "note" || note["cite"] != "S1" {
		t.Errorf("note source = %v, want kind=note cite=S1", note)
	}

	// wiki source — kind inferred from a bare ref.
	wiki := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_source",
		map[string]any{"id": id, "ref": "프로젝트/topsolar.md"}))
	if wiki["kind"] != "wiki" || wiki["ref"] != "프로젝트/topsolar.md" {
		t.Errorf("wiki source = %v, want kind=wiki + ref", wiki)
	}

	got := decodePayload(t, callNotebook(t, m, "miniapp.notebook.get", map[string]any{"id": id}))
	if srcs, _ := got["sources"].([]any); len(srcs) != 2 {
		t.Errorf("get returned %d sources, want 2", len(srcs))
	}
}

func TestNotebookAddSourceRejections(t *testing.T) {
	m := notebookTestMethods(t)

	if resp := callNotebook(t, m, "miniapp.notebook.add_source", map[string]any{"kind": "note", "text": "x"}); resp.OK {
		t.Error("add_source without id should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_source", map[string]any{"id": "nope", "kind": "note", "text": "x"}); resp.OK {
		t.Error("add_source to an unknown notebook should fail")
	}

	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	if resp := callNotebook(t, m, "miniapp.notebook.add_source", map[string]any{"id": id, "kind": "note"}); resp.OK {
		t.Error("note source without text should fail validation")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.create", map[string]any{"description": "no name"}); resp.OK {
		t.Error("create without a name should fail")
	}
}

func TestNotebookRemoveSource(t *testing.T) {
	m := notebookTestMethods(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	for _, txt := range []string{"첫째", "둘째"} {
		decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_source",
			map[string]any{"id": id, "kind": "note", "text": txt}))
	}

	// Remove S1 → the updated notebook keeps only S2 (cites are stable; gaps OK).
	out := decodePayload(t, callNotebook(t, m, "miniapp.notebook.remove_source", map[string]any{"id": id, "cite": "S1"}))
	srcs, _ := out["sources"].([]any)
	if len(srcs) != 1 {
		t.Fatalf("after remove, %d sources, want 1", len(srcs))
	}
	if first, _ := srcs[0].(map[string]any); first["cite"] != "S2" {
		t.Errorf("remaining cite = %v, want S2", srcs[0])
	}
	if resp := callNotebook(t, m, "miniapp.notebook.remove_source", map[string]any{"id": id, "cite": "S9"}); resp.OK {
		t.Error("removing an unknown cite should fail")
	}
}

func TestNotebookDelete(t *testing.T) {
	m := notebookTestMethods(t)
	a := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "A"}))
	decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "B"}))
	idA, _ := a["id"].(string)

	// Delete A → the returned list has just B left.
	out := decodePayload(t, callNotebook(t, m, "miniapp.notebook.delete", map[string]any{"id": idA}))
	if nbs, _ := out["notebooks"].([]any); len(nbs) != 1 {
		t.Fatalf("after delete, %d notebooks, want 1", len(nbs))
	}
	if resp := callNotebook(t, m, "miniapp.notebook.get", map[string]any{"id": idA}); resp.OK {
		t.Error("get on a deleted notebook should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.delete", map[string]any{"id": "nope"}); resp.OK {
		t.Error("deleting an unknown notebook should fail")
	}
}
