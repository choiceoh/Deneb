package handlerminiapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// topicDocsTestDeps wires a single-topic editor over a temp dir with a fixed
// "업무" key, mirroring the live {"0":"업무"} mapping.
func topicDocsTestDeps(dir, key string) TopicDocsDeps {
	return TopicDocsDeps{
		TopicsDir:  func() (string, error) { return dir, nil },
		CurrentKey: func() string { return key },
	}
}

func TestTopicDocsMethods_RegistersConditionally(t *testing.T) {
	got := TopicDocsMethods(topicDocsTestDeps(t.TempDir(), "업무"))
	for _, name := range []string{
		"miniapp.topicdocs.read_current",
		"miniapp.topicdocs.write_current",
	} {
		if _, ok := got[name]; !ok {
			t.Errorf("Methods() missing %q", name)
		}
	}
	// Both factories required — a nil either side disables the surface.
	if TopicDocsMethods(TopicDocsDeps{CurrentKey: func() string { return "업무" }}) != nil {
		t.Error("nil TopicsDir factory should yield nil methods")
	}
	if TopicDocsMethods(TopicDocsDeps{TopicsDir: func() (string, error) { return "/x", nil }}) != nil {
		t.Error("nil CurrentKey factory should yield nil methods")
	}
}

func TestTopicDocsReadCurrent_EmptyWhenFileMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "topics") // not created yet
	h := TopicDocsMethods(topicDocsTestDeps(dir, "업무"))["miniapp.topicdocs.read_current"]
	got := decodePayload(t, h(authedCtx(), newReq(t, "miniapp.topicdocs.read_current")))
	if got["content"] != "" {
		t.Errorf("missing file should yield empty content, got %#v", got["content"])
	}
	if got["key"] != "업무" {
		t.Errorf("key = %v, want 업무", got["key"])
	}
	if got["name"] != "업무.md" {
		t.Errorf("name = %v, want 업무.md", got["name"])
	}
}

func TestTopicDocsWriteCurrent_CreatesDirAndReadsBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "topics") // missing → write must create it
	methods := TopicDocsMethods(topicDocsTestDeps(dir, "업무"))
	body := "# 업무 배경지식\n비밀 ZEPHYR_7788"

	wOut := decodePayload(t, methods["miniapp.topicdocs.write_current"](
		authedCtx(), reqWith(t, "miniapp.topicdocs.write_current",
			map[string]any{"content": body}),
	))
	if wOut["name"] != "업무.md" {
		t.Errorf("write name = %v, want 업무.md", wOut["name"])
	}
	// applyNow defaults off, and no ApplyNow hook is wired → applied=false.
	if wOut["applied"] != false {
		t.Errorf("applied = %v, want false (deferred default)", wOut["applied"])
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("topics dir not auto-created: %v", err)
	}
	// The file is written under the resolved key, not any client-named path.
	if _, err := os.Stat(filepath.Join(dir, "업무.md")); err != nil {
		t.Errorf("expected 업무.md to exist: %v", err)
	}

	rOut := decodePayload(t, methods["miniapp.topicdocs.read_current"](
		authedCtx(), reqWith(t, "miniapp.topicdocs.read_current", map[string]any{}),
	))
	if rOut["content"] != body {
		t.Errorf("content roundtrip mismatch: got %q want %q", rOut["content"], body)
	}
}

func TestTopicDocsWriteCurrent_ApplyNowInvokesHook(t *testing.T) {
	dir := t.TempDir()
	called := 0
	deps := topicDocsTestDeps(dir, "업무")
	deps.ApplyNow = func() { called++ }
	h := TopicDocsMethods(deps)["miniapp.topicdocs.write_current"]

	// applyNow=false → hook not called, applied=false.
	out := decodePayload(t, h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_current",
		map[string]any{"content": "x", "applyNow": false})))
	if called != 0 || out["applied"] != false {
		t.Errorf("applyNow=false should not invoke hook: called=%d applied=%v", called, out["applied"])
	}

	// applyNow=true → hook called once, applied=true.
	out = decodePayload(t, h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_current",
		map[string]any{"content": "y", "applyNow": true})))
	if called != 1 || out["applied"] != true {
		t.Errorf("applyNow=true should invoke hook once: called=%d applied=%v", called, out["applied"])
	}
}

func TestTopicDocsWriteCurrent_RejectsEmpty(t *testing.T) {
	h := TopicDocsMethods(topicDocsTestDeps(t.TempDir(), "업무"))["miniapp.topicdocs.write_current"]
	resp := h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_current",
		map[string]any{"content": "   "}))
	if resp.OK {
		t.Fatal("expected error for empty/whitespace content")
	}
}

func TestTopicDocsWriteCurrent_RejectsOverCap(t *testing.T) {
	h := TopicDocsMethods(topicDocsTestDeps(t.TempDir(), "업무"))["miniapp.topicdocs.write_current"]
	big := strings.Repeat("x", maxTopicDocBytes+1)
	resp := h(authedCtx(), reqWith(t, "miniapp.topicdocs.write_current",
		map[string]any{"content": big}))
	if resp.OK {
		t.Fatal("expected validation error for content over the injection cap")
	}
}

// A malformed config key (path traversal) must be refused even though the key is
// gateway-resolved, never client-supplied — defense in depth so a bad map entry
// cannot escape the topics dir.
func TestTopicDocs_RejectsUnsafeCurrentKey(t *testing.T) {
	for _, badKey := range []string{"../secret", "a/b", "..", ".hidden", ""} {
		methods := TopicDocsMethods(topicDocsTestDeps(t.TempDir(), badKey))
		if methods == nil {
			t.Fatalf("methods unexpectedly nil for key %q", badKey)
		}
		if resp := methods["miniapp.topicdocs.read_current"](authedCtx(),
			newReq(t, "miniapp.topicdocs.read_current")); resp.OK {
			t.Errorf("read accepted unsafe key %q", badKey)
		}
		if resp := methods["miniapp.topicdocs.write_current"](authedCtx(),
			reqWith(t, "miniapp.topicdocs.write_current", map[string]any{"content": "x"})); resp.OK {
			t.Errorf("write accepted unsafe key %q", badKey)
		}
	}
}

func TestTopicDocs_RequiresAuth(t *testing.T) {
	methods := TopicDocsMethods(topicDocsTestDeps(t.TempDir(), "업무"))
	for name, h := range methods {
		resp := h(context.Background(), newReq(t, name)) // no client identity
		if resp.OK {
			t.Errorf("%s allowed without client identity", name)
		}
		if resp.Error.Code != protocol.ErrUnauthorized {
			t.Errorf("%s code = %s, want %s", name, resp.Error.Code, protocol.ErrUnauthorized)
		}
	}
}
