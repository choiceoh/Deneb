package knowledge

import (
	"context"
	"encoding/base64"
	"errors"
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
		OcrImage: func(_ context.Context, data []byte) (string, error) {
			if string(data) == "ocr-fail" {
				return "", errors.New("ocr sidecar down")
			}
			return "OCR: " + string(data), nil
		},
		Transcribe: func(_ context.Context, data []byte, mime, _ string) (string, error) {
			return "ASR(" + mime + "): " + string(data), nil
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

// notebookTestMethodsWithReaders wires fake url/mail/diary readers so add_ref
// registers. The url reader echoes the ref as text, mail returns a fixed body,
// and diary is intentionally left nil to assert the per-kind unavailable branch.
func notebookTestMethodsWithReaders(t *testing.T) map[string]rpcutil.HandlerFunc {
	t.Helper()
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NotebookMethods(NotebookDeps{
		Store: func() (*notebook.Store, error) { return store, nil },
		FetchURL: func(_ context.Context, ref string) (string, error) {
			if ref == "https://boom.example" {
				return "", errors.New("안전하지 않은 URL")
			}
			return "본문 of " + ref, nil
		},
		ReadMail: func(_ context.Context, ref string) (string, error) {
			return "메일 스레드 " + ref + " 본문", nil
		},
		// ReadDiary left nil on purpose.
	})
}

// TestNotebookAddRefIngestsUrlAndMail exercises the ref-ingestion path: url and
// mail refs are read server-side and pinned with the fetched text (not a bare ref).
func TestNotebookAddRefIngestsUrlAndMail(t *testing.T) {
	m := notebookTestMethodsWithReaders(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)

	url := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_ref",
		map[string]any{"id": id, "kind": "url", "ref": "https://example.com/a"}))
	if url["kind"] != "url" || url["ref"] != "https://example.com/a" {
		t.Errorf("url source = %v, want kind=url + ref", url)
	}
	if url["text"] != "본문 of https://example.com/a" {
		t.Errorf("url text = %v, want the fetched body", url["text"])
	}
	if url["title"] != "https://example.com/a" {
		t.Errorf("url title = %v, want the ref as default title", url["title"])
	}

	mail := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_ref",
		map[string]any{"id": id, "kind": "mail", "ref": "thread-7", "title": "협상 메일"}))
	if mail["kind"] != "mail" || mail["title"] != "협상 메일" {
		t.Errorf("mail source = %v, want kind=mail + explicit title", mail)
	}
}

func TestNotebookAddRefRejections(t *testing.T) {
	m := notebookTestMethodsWithReaders(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)

	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"kind": "url", "ref": "https://x"}); resp.OK {
		t.Error("add_ref without id should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"id": id, "kind": "url"}); resp.OK {
		t.Error("add_ref without ref should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"id": id, "kind": "note", "ref": "x"}); resp.OK {
		t.Error("add_ref with an unsupported kind should fail")
	}
	// diary reader is nil → the kind is unavailable even though add_ref is registered.
	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"id": id, "kind": "diary", "ref": "2026-06-22"}); resp.OK {
		t.Error("add_ref for a kind whose reader is nil should fail as unavailable")
	}
	// A reader error (unsafe URL) becomes a graceful failure, not a pin.
	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"id": id, "kind": "url", "ref": "https://boom.example"}); resp.OK {
		t.Error("add_ref whose reader errors should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.add_ref", map[string]any{"id": "nope", "kind": "url", "ref": "https://x"}); resp.OK {
		t.Error("add_ref to an unknown notebook should fail")
	}
}

// TestNotebookAddRefOmittedWithoutReaders pins the conditional registration:
// with no readers wired, add_ref must not be a registered method.
func TestNotebookAddRefOmittedWithoutReaders(t *testing.T) {
	m := notebookTestMethods(t)
	if _, ok := m["miniapp.notebook.add_ref"]; ok {
		t.Error("add_ref should not register when no url/mail/diary reader is wired")
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

// TestNotebookAddFileDispatchesByMime proves add_file routes an image to OCR and
// an audio/video file to ASR (not the document extractor), so a photo of a
// contract or a meeting recording becomes grounding text.
func TestNotebookAddFileDispatchesByMime(t *testing.T) {
	m := notebookTestMethodsWithExtractor(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)

	img := base64.StdEncoding.EncodeToString([]byte("계약서 사진"))
	imgSrc := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "scan.png", "mimeType": "image/png", "dataBase64": img}))
	if imgSrc["kind"] != "file" || imgSrc["text"] != "OCR: 계약서 사진" {
		t.Errorf("image source = %v, want OCR-extracted text", imgSrc)
	}

	audio := base64.StdEncoding.EncodeToString([]byte("회의 녹음"))
	audSrc := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "memo.m4a", "mimeType": "audio/mp4", "dataBase64": audio}))
	if audSrc["text"] != "ASR(audio/mp4): 회의 녹음" {
		t.Errorf("audio source = %v, want ASR transcript", audSrc["text"])
	}

	// A video routes to ASR too (ffmpeg pulls the audio track downstream).
	vid := base64.StdEncoding.EncodeToString([]byte("영상 회의"))
	vidSrc := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "call.mp4", "mimeType": "video/mp4", "dataBase64": vid}))
	if vidSrc["text"] != "ASR(video/mp4): 영상 회의" {
		t.Errorf("video source = %v, want ASR transcript", vidSrc["text"])
	}

	// An OCR failure is a graceful add failure, not a document-extractor fallback.
	ocrFail := base64.StdEncoding.EncodeToString([]byte("ocr-fail"))
	if resp := callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "bad.png", "mimeType": "image/png", "dataBase64": ocrFail}); resp.OK {
		t.Error("add_file with an OCR failure should fail, not fall back")
	}
}

// TestNotebookAddFilePinsOriginalPathAsRef proves add_file archives the raw bytes
// via SaveOriginal and pins the returned retrievable path as the source ref (so the
// client can re-open the original), while the title still reads as the filename.
func TestNotebookAddFilePinsOriginalPathAsRef(t *testing.T) {
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	var savedName string
	m := NotebookMethods(NotebookDeps{
		Store:       func() (*notebook.Store, error) { return store, nil },
		ExtractText: func(_ context.Context, data []byte, _, _ string) string { return string(data) },
		SaveOriginal: func(_ context.Context, id, filename string, _ []byte) (string, error) {
			savedName = filename
			return "노트북/" + id + "/" + filename, nil
		},
	})
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	blob := base64.StdEncoding.EncodeToString([]byte("계약 본문"))
	src := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "계약서.pdf", "dataBase64": blob}))
	if savedName != "계약서.pdf" {
		t.Errorf("SaveOriginal got filename %q, want 계약서.pdf", savedName)
	}
	if src["ref"] != "노트북/"+id+"/계약서.pdf" {
		t.Errorf("ref = %v, want the archived file-store path", src["ref"])
	}
	if src["title"] != "계약서.pdf" {
		t.Errorf("title = %v, want the filename (not the path)", src["title"])
	}
}

// TestNotebookAddFileFallsBackToFilenameRefWhenArchiveFails proves a SaveOriginal
// failure is non-fatal: the source still pins with the bare filename as its ref.
func TestNotebookAddFileFallsBackToFilenameRefWhenArchiveFails(t *testing.T) {
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NotebookMethods(NotebookDeps{
		Store:        func() (*notebook.Store, error) { return store, nil },
		ExtractText:  func(_ context.Context, data []byte, _, _ string) string { return string(data) },
		SaveOriginal: func(_ context.Context, _, _ string, _ []byte) (string, error) { return "", errors.New("disk full") },
	})
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	blob := base64.StdEncoding.EncodeToString([]byte("본문"))
	src := decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_file",
		map[string]any{"id": id, "filename": "a.pdf", "dataBase64": blob}))
	if src["ref"] != "a.pdf" {
		t.Errorf("ref = %v, want the filename fallback when archiving fails", src["ref"])
	}
}

// TestNotebookAddFileRegistersWithOnlyOCR proves the method registers when only a
// non-document extractor is wired (document sidecar absent, OCR present).
func TestNotebookAddFileRegistersWithOnlyOCR(t *testing.T) {
	t.Helper()
	store, err := notebook.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m := NotebookMethods(NotebookDeps{
		Store:    func() (*notebook.Store, error) { return store, nil },
		OcrImage: func(_ context.Context, _ []byte) (string, error) { return "x", nil },
	})
	if _, ok := m["miniapp.notebook.add_file"]; !ok {
		t.Error("add_file should register when only OCR is wired")
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

// TestNotebookEditSourceRenamesKeepingCite proves edit_source updates a source's
// title while keeping its cite (citations stay stable) and other fields.
func TestNotebookEditSourceRenamesKeepingCite(t *testing.T) {
	m := notebookTestMethods(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_source",
		map[string]any{"id": id, "kind": "note", "text": "잔금 5%"}))

	edited := decodePayload(t, callNotebook(t, m, "miniapp.notebook.edit_source",
		map[string]any{"id": id, "cite": "S1", "title": "잔금 조건"}))
	if edited["cite"] != "S1" || edited["title"] != "잔금 조건" {
		t.Errorf("edit_source = %v, want cite=S1 title=잔금 조건", edited)
	}
	if edited["text"] != "잔금 5%" {
		t.Errorf("edit_source dropped the text: %v", edited["text"])
	}

	// The rename persists in a subsequent get.
	got := decodePayload(t, callNotebook(t, m, "miniapp.notebook.get", map[string]any{"id": id}))
	srcs, _ := got["sources"].([]any)
	if first, _ := srcs[0].(map[string]any); first["title"] != "잔금 조건" {
		t.Errorf("get after edit = %v, want the new title", srcs[0])
	}
}

func TestNotebookEditSourceRejections(t *testing.T) {
	m := notebookTestMethods(t)
	created := decodePayload(t, callNotebook(t, m, "miniapp.notebook.create", map[string]any{"name": "딜"}))
	id, _ := created["id"].(string)
	decodePayload(t, callNotebook(t, m, "miniapp.notebook.add_source", map[string]any{"id": id, "kind": "note", "text": "x"}))

	if resp := callNotebook(t, m, "miniapp.notebook.edit_source", map[string]any{"cite": "S1", "title": "y"}); resp.OK {
		t.Error("edit_source without id should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.edit_source", map[string]any{"id": id, "title": "y"}); resp.OK {
		t.Error("edit_source without cite should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.edit_source", map[string]any{"id": id, "cite": "S9", "title": "y"}); resp.OK {
		t.Error("edit_source of an unknown cite should fail")
	}
	if resp := callNotebook(t, m, "miniapp.notebook.edit_source", map[string]any{"id": "nope", "cite": "S1", "title": "y"}); resp.OK {
		t.Error("edit_source in an unknown notebook should fail")
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
