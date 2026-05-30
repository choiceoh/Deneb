package handlerminiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func topicDocsTestDeps(dir string) TopicDocsDeps {
	return TopicDocsDeps{TopicsDir: func() (string, error) { return dir, nil }}
}

func mustWriteTopicFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestTopicDocsMethods_RegistersAll(t *testing.T) {
	got := TopicDocsMethods(topicDocsTestDeps(t.TempDir()))
	for _, name := range []string{
		"miniapp.topicdocs.list_files",
		"miniapp.topicdocs.read_file",
		"miniapp.topicdocs.write_file",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("Methods() missing %q", name)
		}
	}
	if TopicDocsMethods(TopicDocsDeps{}) != nil {
		t.Error("nil TopicsDir factory should yield nil methods (conditional registration)")
	}
}

func TestTopicDocsList_EmptyWhenDirMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "topics") // not created yet
	h := TopicDocsMethods(topicDocsTestDeps(dir))["miniapp.topicdocs.list_files"]
	got := decodePayload(t, h(authedCtx(), newReq(t, "miniapp.topicdocs.list_files")))
	files, _ := got["files"].([]any)
	if len(files) != 0 {
		t.Errorf("missing dir should yield empty files, got %#v", got["files"])
	}
}

func TestTopicDocsList_FiltersAndSorts(t *testing.T) {
	dir := t.TempDir()
	mustWriteTopicFile(t, dir, "coding.md", "a")
	mustWriteTopicFile(t, dir, "work.md", "b")
	mustWriteTopicFile(t, dir, "notes.txt", "c")  // non-.md excluded
	mustWriteTopicFile(t, dir, ".hidden.md", "d") // dotfile excluded
	if err := os.Mkdir(filepath.Join(dir, "sub.md"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	h := TopicDocsMethods(topicDocsTestDeps(dir))["miniapp.topicdocs.list_files"]
	got := decodePayload(t, h(authedCtx(), newReq(t, "miniapp.topicdocs.list_files")))
	files, _ := got["files"].([]any)
	if len(files) != 2 {
		t.Fatalf("want 2 files (coding/work), got %d: %#v", len(files), got["files"])
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.(map[string]any)["name"].(string)] = true
	}
	if !names["coding.md"] || !names["work.md"] {
		t.Errorf("expected coding.md + work.md, got %v", names)
	}
}

func TestTopicDocsWrite_CreatesDirAndReadsBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "topics") // missing → write must create it
	methods := TopicDocsMethods(topicDocsTestDeps(dir))
	body := "# coding\n비밀 ZEPHYR_7788"

	wOut := decodePayload(t, methods["miniapp.topicdocs.write_file"](
		authedCtx(), reqWith(t, "miniapp.topicdocs.write_file",
			map[string]any{"name": "coding.md", "content": body})))
	if wOut["created"] != true {
		t.Errorf("created = %v, want true", wOut["created"])
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("topics dir not auto-created: %v", err)
	}

	rOut := decodePayload(t, methods["miniapp.topicdocs.read_file"](
		authedCtx(), reqWith(t, "miniapp.topicdocs.read_file",
			map[string]any{"name": "coding.md"})))
	if rOut["content"] != body {
		t.Errorf("content roundtrip mismatch: got %q want %q", rOut["content"], body)
	}
}

func TestTopicDocsRead_NotFound(t *testing.T) {
	h := TopicDocsMethods(topicDocsTestDeps(t.TempDir()))["miniapp.topicdocs.read_file"]
	resp := h(authedCtx(), reqWith(t, "miniapp.topicdocs.read_file", map[string]any{"name": "nope.md"}))
	if resp.OK {
		t.Fatal("expected error for missing file")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

func TestTopicDocsWrite_CreateConflict(t *testing.T) {
	dir := t.TempDir()
	mustWriteTopicFile(t, dir, "coding.md", "existing")
	h := TopicDocsMethods(topicDocsTestDeps(dir))["miniapp.topicdocs.write_file"]
	resp := h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_file",
		map[string]any{"name": "coding.md", "content": "x", "create": true}))
	if resp.OK {
		t.Fatal("expected conflict for create=true on an existing file")
	}
}

func TestTopicDocsWrite_TooLarge(t *testing.T) {
	h := TopicDocsMethods(topicDocsTestDeps(t.TempDir()))["miniapp.topicdocs.write_file"]
	big := strings.Repeat("x", maxTopicDocBytes+1)
	resp := h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_file",
		map[string]any{"name": "big.md", "content": big}))
	if resp.OK {
		t.Fatal("expected validation error for content over 512KB")
	}
}

func TestTopicDocs_RejectsBadNames(t *testing.T) {
	methods := TopicDocsMethods(topicDocsTestDeps(t.TempDir()))
	for _, name := range []string{"../secret.md", "a/b.md", "..", "foo.txt", "/etc/passwd", ".hidden.md", "no-ext"} {
		if resp := methods["miniapp.topicdocs.read_file"](authedCtx(),
			reqWith(t, "miniapp.topicdocs.read_file", map[string]any{"name": name})); resp.OK {
			t.Errorf("read accepted bad name %q", name)
		}
		if resp := methods["miniapp.topicdocs.write_file"](authedCtx(),
			reqWith(t, "miniapp.topicdocs.write_file", map[string]any{"name": name, "content": "x"})); resp.OK {
			t.Errorf("write accepted bad name %q", name)
		}
	}
}

func TestTopicDocs_RequiresAuth(t *testing.T) {
	methods := TopicDocsMethods(topicDocsTestDeps(t.TempDir()))
	for name, h := range methods {
		resp := h(context.Background(), newReq(t, name)) // no initData
		if resp.OK {
			t.Errorf("%s allowed without initData", name)
		}
		if resp.Error.Code != protocol.ErrUnauthorized {
			t.Errorf("%s code = %s, want %s", name, resp.Error.Code, protocol.ErrUnauthorized)
		}
	}
}
