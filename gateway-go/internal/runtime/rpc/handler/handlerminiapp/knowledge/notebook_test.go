package knowledge

import (
	"context"
	"encoding/base64"
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

// notebookTestMethodsWithExtractor wires a fake extractor so add_file registers.
// The fake echoes decoded bytes as "text" and returns empty for a sentinel blob,
// letting the test drive both the happy path and the no-text-extracted rejection.
func notebookTestMethodsWithExtractor(t *testing.T) map[string]rpcutil.HandlerFunc {
	t.Helper()
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NotebookMethods(NotebookDeps{
		Store: func() (*notebook.Store, error) { return store, nil },
		ExtractText: func(_ context.Context, data []byte, _, _ string) string {
			if string(data) == "no-text" {
				return ""
			}
			return string(data)
		},
	})
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

// TestNotebookAddFileExtractsAndPins exercises the picked-file path: a base64
// document is extracted server-side and pinned as a kind=file source with the
// filename as ref/title — no path typed by the user.
func TestNotebookAddFileExtractsAndPins(t *testing.T) {
	m := notebookTestMethodsWithExtractor(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)

	blob := base64.StdEncoding.EncodeToString([]byte("계약서 본문 텍스트"))
	src := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "계약서.pdf", "dataBase64": blob}))
	if src["kind"] != "file" || src["cite"] != "S1" {
		t.Errorf("add_file source = %v, want kind=file cite=S1", src)
	}
	if src["ref"] != "계약서.pdf" || src["title"] != "계약서.pdf" {
		t.Errorf("add_file ref/title = %v/%v, want the filename for both", src["ref"], src["title"])
	}
	if src["text"] != "계약서 본문 텍스트" {
		t.Errorf("add_file text = %v, want the extracted text", src["text"])
	}

	// An explicit title overrides the filename default; a data-URL prefix is tolerated.
	src = decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "견적.xlsx", "title": "1차 견적", "dataBase64": "data:application/octet-stream;base64," + base64.StdEncoding.EncodeToString([]byte("견적 표"))}))
	if src["title"] != "1차 견적" {
		t.Errorf("add_file title = %v, want the explicit title", src["title"])
	}
}

func TestNotebookAddFileRejections(t *testing.T) {
	m := notebookTestMethodsWithExtractor(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)

	goodBlob := base64.StdEncoding.EncodeToString([]byte("x"))
	if resp := callNotebook(t, m, "miniapp.notebook.add_file", map[string]any{"filename": "a.pdf", "dataBase64": goodBlob}); resp.OK {
		t.Error("add_file without id should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_file", map[string]any{"id": id, "filename": "a.pdf"}); resp.OK {
		t.Error("add_file without dataBase64 should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_file", map[string]any{"id": id, "filename": "a.pdf", "dataBase64": "!!not base64!!"}); resp.OK {
		t.Error("add_file with invalid base64 should fail")
	}
	// The extractor returns "" for the "no-text" sentinel → unextractable file rejected.
	noText := base64.StdEncoding.EncodeToString([]byte("no-text"))
	if resp := callNotebook(t, m, "miniapp.notebook.add_file", map[string]any{"id": id, "filename": "scan.pdf", "dataBase64": noText}); resp.OK {
		t.Error("add_file whose file yields no text should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_file", map[string]any{"id": "nope", "filename": "a.pdf", "dataBase64": goodBlob}); resp.OK {
		t.Error("add_file to an unknown notebook should fail")
	}
}

// TestNotebookAddFileOmittedWithoutExtractor pins the conditional registration:
// with no extractor wired, add_file must not be a registered method (clients then
// fall back to the note/wiki source kinds instead of hitting a broken surface).
func TestNotebookAddFileOmittedWithoutExtractor(t *testing.T) {
	m := notebookTestMethods(t)
	if _, ok := m["miniapp.notebook.add_file"]; ok {
		t.Error("add_file should not register when ExtractText is nil")
	}
}

func TestNotebookRemoveSourceDropsCitedEntryAndRejectsUnknownCite(t *testing.T) {
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

// TestNotebookSetModeTogglesStrictSoftAndRejectsInvalidInputs exercises the grounding-strictness toggle the desktop
// notebook pane drives: new notebooks are soft (mode omitted), set_mode switches
// to strict and back, and bad mode / unknown id / missing id are rejected.
func TestNotebookSetModeTogglesStrictSoftAndRejectsInvalidInputs(t *testing.T) {
	m := notebookTestMethods(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create returned no id: %v", created)
	}

	// New notebooks default to soft, so mode is omitted (omitempty) from get.
	got := decodePayload(t, callNotebook(t, m, "miniapp.notebook.get", map[string]any{"id": id}))
	if mode, ok := got["mode"]; ok && mode != "" {
		t.Errorf("new notebook mode = %v, want soft (omitted)", mode)
	}

	// Toggle to strict → the returned notebook reflects it immediately.
	out := decodePayload(t, callNotebook(t, m, "miniapp.notebook.set_mode", map[string]any{"id": id, "mode": "strict"}))
	if out["mode"] != "strict" {
		t.Errorf("after set_mode strict, mode = %v, want strict", out["mode"])
	}

	// Back to soft → mode omitted again.
	out = decodePayload(t, callNotebook(t, m, "miniapp.notebook.set_mode", map[string]any{"id": id, "mode": "soft"}))
	if mode, ok := out["mode"]; ok && mode != "" {
		t.Errorf("after set_mode soft, mode = %v, want soft (omitted)", mode)
	}

	if resp := callNotebook(t, m, "miniapp.notebook.set_mode", map[string]any{"id": id, "mode": "bogus"}); resp.OK {
		t.Error("set_mode with an invalid mode should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.set_mode", map[string]any{"id": "nope", "mode": "strict"}); resp.OK {
		t.Error("set_mode on an unknown notebook should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.set_mode", map[string]any{"mode": "strict"}); resp.OK {
		t.Error("set_mode without id should fail")
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
