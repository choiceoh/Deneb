package wikiwork

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/googledrive"
)

// readyStubRunner is a chatport.SyncRunner that reports ready; its RunSync is
// not exercised by the tests below (they return before the agent turn).
type readyStubRunner struct{}

func (readyStubRunner) ChatReady() bool { return true }
func (readyStubRunner) RunSync(context.Context, chatport.SyncRequest) (*chatport.SyncResult, error) {
	return &chatport.SyncResult{}, nil
}

func newTestStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// fakeDrive is an in-memory driveClient for tests.
type fakeDrive struct {
	files    []googledrive.File
	contents map[string][]byte
	listErr  error
	dlErr    map[string]error
	listCall int
}

func (f *fakeDrive) ListFolderFiles(_ context.Context, _, modifiedAfter string) ([]googledrive.File, error) {
	f.listCall++
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []googledrive.File
	for _, file := range f.files {
		if modifiedAfter == "" || file.ModifiedTime > modifiedAfter {
			out = append(out, file)
		}
	}
	return out, nil
}

func (f *fakeDrive) DownloadFile(_ context.Context, id string) ([]byte, error) {
	if f.dlErr != nil {
		if err := f.dlErr[id]; err != nil {
			return nil, err
		}
	}
	return f.contents[id], nil
}

func TestSupernoteConstructionReturnsErrorWhenDepsMissing(t *testing.T) {
	dir := t.TempDir()
	task := NewSupernoteDigestTask(nil, nil, nil, nil,
		filepath.Join(dir, "state.json"), "folder123", "")
	if task == nil {
		t.Fatal("NewSupernoteDigestTask returned nil")
	}
	if task.Name() != "supernote-digest" {
		t.Errorf("Name=%q", task.Name())
	}
	if task.Interval() != SupernoteInterval {
		t.Errorf("Interval=%s", task.Interval())
	}
	// Missing deps → error.
	if err := task.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("disabled Run=%v", err)
	}
}

// TestSupernoteSelectFreshDeduplicatesSeenIDsAndCapsResults pins selection: already-seen IDs skip,
// non-note kinds skip, oldest-first, capped.
func TestSupernoteSelectFreshDeduplicatesSeenIDsAndCapsResults(t *testing.T) {
	task := &supernoteDigestTask{}
	state := &supernoteDigestState{Version: 1, SeenIDs: []string{"old"}}
	files := []googledrive.File{
		{ID: "b", Name: "회의.pdf", MimeType: "application/pdf", ModifiedTime: "2026-07-12T10:00:00Z"},
		{ID: "old", Name: "이미본것.pdf", MimeType: "application/pdf", ModifiedTime: "2026-07-12T09:00:00Z"},
		{ID: "raw", Name: "원본.note", MimeType: "application/octet-stream", ModifiedTime: "2026-07-12T11:00:00Z"},
		{ID: "a", Name: "메모.txt", MimeType: "text/plain", ModifiedTime: "2026-07-12T08:00:00Z"},
	}
	got := task.selectFresh(files, state)
	// Expect a (08:00 txt) then b (10:00 pdf); "old" seen, "raw" .note skipped.
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("selectFresh=%+v", got)
	}

	// Cap.
	task2 := &supernoteDigestTask{}
	var many []googledrive.File
	for i := 0; i < supernoteMaxFilesPerCycle+3; i++ {
		many = append(many, googledrive.File{ID: fmt.Sprintf("f%d", i), Name: "n.pdf", MimeType: "application/pdf", ModifiedTime: fmt.Sprintf("2026-07-12T%02d:00:00Z", i)})
	}
	if got := task2.selectFresh(many, &supernoteDigestState{Version: 1}); len(got) != supernoteMaxFilesPerCycle {
		t.Errorf("cap not applied: %d", len(got))
	}
}

func TestIsIngestibleNoteReturnsTrueOnlyForSupportedFileTypes(t *testing.T) {
	yes := []googledrive.File{
		{Name: "a.pdf", MimeType: "application/pdf"},
		{Name: "b.PDF", MimeType: ""},
		{Name: "c.txt", MimeType: "text/plain"},
		{Name: "d", MimeType: "text/markdown"},
	}
	for _, f := range yes {
		if !isIngestibleNote(f.Name, f.MimeType) {
			t.Errorf("expected ingestible: %+v", f)
		}
	}
	no := []googledrive.File{
		{Name: "e.note", MimeType: "application/octet-stream"},
		{Name: "f.png", MimeType: "image/png"},
	}
	for _, f := range no {
		if isIngestibleNote(f.Name, f.MimeType) {
			t.Errorf("expected NOT ingestible: %+v", f)
		}
	}
}

// TestSupernoteBuildPromptRendersNotesWithAttributionAndBrief pins the consolidation contract: notes render with
// name+content, first-party framing, wiki-write surface, source attribution.
func TestSupernoteBuildPromptRendersNotesWithAttributionAndBrief(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "WIKI.md"), []byte("태양광 프로젝트 우선"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &supernoteDigestTask{workspaceDir: ws}
	notes := []ingestedNote{
		{name: "기아PE 회의.pdf", modified: "2026-07-12T10:00:00Z", text: "발주 다음 주 확정, 김판석 콜백"},
	}
	got := task.buildPrompt(notes)
	for _, want := range []string{
		"손으로 쓴 노트 1건",
		"기아PE 회의.pdf", "발주 다음 주 확정",
		"사용자 본인이 직접 쓴 1차 자료",
		"필기인식", // recognition-error tolerance
		"로그.md에 '## [YYYY-MM-DD]",
		"새 대표페이지를 만들거나",
		"슈퍼노트 <노트명>",
		"태양광 프로젝트 우선", // operator brief injected
		"운영자 위키 지침",
		"사용자에게 알리지 마세요",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

// TestSupernoteRunReturnsWithoutDriveCallWhenFolderUnconfigured pins the dormant no-op: no folder → nil,
// no Drive call.
func TestSupernoteRunReturnsWithoutDriveCallWhenFolderUnconfigured(t *testing.T) {
	fd := &fakeDrive{}
	// Folder empty ⇒ the task returns before ever building a Drive client.
	tk := &supernoteDigestTask{
		chatHandler: readyStubRunner{},
		wikiStore:   newTestStore(t),
		logger:      testWikiLogger(),
		folderID:    "",
		newDrive:    func() (driveClient, error) { return fd, nil },
	}
	if err := tk.Run(context.Background()); err != nil {
		t.Fatalf("unconfigured Run=%v", err)
	}
	if fd.listCall != 0 {
		t.Error("Drive listed despite unconfigured folder")
	}
}

// TestSupernoteCursorAdvancesOnUnreadableBatch pins that a batch yielding no
// usable text still advances the cursor so it isn't re-downloaded forever.
func TestSupernoteCursorAdvancesOnUnreadableBatch(t *testing.T) {
	fd := &fakeDrive{
		files:    []googledrive.File{{ID: "x", Name: "빈.pdf", MimeType: "application/pdf", ModifiedTime: "2026-07-12T10:00:00Z"}},
		contents: map[string][]byte{"x": []byte("%PDF-empty")},
	}
	dir := t.TempDir()
	tk := &supernoteDigestTask{
		chatHandler: readyStubRunner{},
		wikiStore:   newTestStore(t),
		logger:      testWikiLogger(),
		statePath:   filepath.Join(dir, "state.json"),
		folderID:    "folder123",
		newDrive:    func() (driveClient, error) { return fd, nil },
		extractText: func(_ context.Context, _ []byte, _, _ string) (string, error) { return "", nil }, // no text
	}
	if err := tk.Run(context.Background()); err != nil {
		t.Fatalf("Run=%v", err)
	}
	st := tk.loadState()
	if st.Cursor != "2026-07-12T10:00:00Z" {
		t.Errorf("cursor not advanced past unreadable file: %q", st.Cursor)
	}
	if len(st.SeenIDs) != 1 || st.SeenIDs[0] != "x" {
		t.Errorf("processed id not recorded: %+v", st.SeenIDs)
	}
}

func TestSupernoteStateBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	task := &supernoteDigestTask{logger: testWikiLogger(), statePath: path}
	fresh := task.loadState()
	if fresh.Version != 1 {
		t.Errorf("fresh=%+v", fresh)
	}
	want := &supernoteDigestState{Version: 1, Cursor: "2026-07-12T10:00:00Z", SeenIDs: []string{"a", "b"}}
	if err := task.saveState(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode=%o", info.Mode().Perm())
	}
	got := task.loadState()
	if got.Cursor != want.Cursor || len(got.SeenIDs) != 2 {
		t.Errorf("loaded=%+v", got)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := task.loadState(); got.Version != 1 {
		t.Errorf("corrupt fallback=%+v", got)
	}
}
