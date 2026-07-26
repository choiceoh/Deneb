package runtimeops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// actionRecorder captures dispatched P1 actions for assertions.
type actionRecorder struct {
	action string
	args   map[string]string
	calls  int
}

func (r *actionRecorder) send(_ context.Context, action string, args map[string]string) error {
	r.action, r.args = action, args
	r.calls++
	return nil
}

// A stale/absent state cache must trigger one sync_state dispatch and return
// retry guidance, never an opaque error (the agent should keep the turn alive).
func TestPhoneRead_StaleCacheRequestsSyncState(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir()) // empty state dir → no cache
	rec := &actionRecorder{}
	read := ToolPhoneRead(rec.send)

	out, err := read(context.Background(), json.RawMessage(`{"what":"battery"}`))
	if err != nil {
		t.Fatalf("stale battery read must not error: %v", err)
	}
	if rec.calls != 1 || rec.action != "sync_state" {
		t.Fatalf("expected one sync_state dispatch, got %d× %q", rec.calls, rec.action)
	}
	if !strings.Contains(out, "다시 호출") {
		t.Errorf("stale reply missing retry guidance: %q", out)
	}
}

func TestPhoneRead_RetiredWhatsReturnGuidance(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	read := ToolPhoneRead(nil)
	for what, want := range map[string]string{
		"clipboard": "지원이 종료",
		"contacts":  "contacts",
	} {
		out, err := read(context.Background(), json.RawMessage(`{"what":"`+what+`"}`))
		if err != nil {
			t.Fatalf("%s: retired what must not error: %v", what, err)
		}
		if !strings.Contains(out, want) {
			t.Errorf("%s guidance missing %q: %q", what, want, out)
		}
	}
}

// The SSH-era names must normalize onto their P1 successors and dispatch to
// the app channel — no Termux anywhere.
func TestPhoneWrite_LegacyNamesRouteToAppActions(t *testing.T) {
	rec := &actionRecorder{}
	write := ToolPhoneWrite(rec.send)

	out, err := write(context.Background(),
		json.RawMessage(`{"to":"notification","title":"회의","text":"10분 후 시작"}`))
	if err != nil {
		t.Fatalf("notification: %v", err)
	}
	if rec.action != "notify" || rec.args["title"] != "회의" || rec.args["text"] != "10분 후 시작" {
		t.Fatalf("notification not routed to notify action: %q %v", rec.action, rec.args)
	}
	if !strings.Contains(out, "launched on device") {
		t.Errorf("unexpected reply: %q", out)
	}

	if _, err := write(context.Background(), json.RawMessage(`{"to":"tts","text":"안녕하세요"}`)); err != nil {
		t.Fatalf("tts: %v", err)
	}
	if rec.action != "speak" || rec.args["text"] != "안녕하세요" {
		t.Fatalf("tts not routed to speak action: %q %v", rec.action, rec.args)
	}

	if _, err := write(context.Background(), json.RawMessage(`{"to":"clipboard","text":"복사할 내용"}`)); err != nil {
		t.Fatalf("clipboard: %v", err)
	}
	if rec.action != "clipboard" || rec.args["text"] != "복사할 내용" {
		t.Fatalf("clipboard set not routed: %q %v", rec.action, rec.args)
	}
}

func TestPhoneWrite_TextRequiredForAppOps(t *testing.T) {
	write := ToolPhoneWrite((&actionRecorder{}).send)
	for _, to := range []string{"notify", "speak", "clipboard"} {
		if _, err := write(context.Background(), json.RawMessage(`{"to":"`+to+`"}`)); err == nil {
			t.Errorf("%s without text must error", to)
		}
	}
}

// Call log was retired with Termux and revived on the push-and-cache path
// (2026-07-26). Its contract is the same as usage/location: serve the cache when
// fresh, otherwise ask the app for a push rather than erroring.
func TestPhoneRead_CallLogServesCacheThenAsksForSync(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)

	rec := &actionRecorder{}
	read := ToolPhoneRead(rec.send)

	out, err := read(context.Background(), json.RawMessage(`{"what":"calllog"}`))
	if err != nil {
		t.Fatalf("empty cache must not error: %v", err)
	}
	if !strings.Contains(out, "갱신을 요청") {
		t.Errorf("stale call log should request a sync_state push, got %q", out)
	}

	digest := "김성훈(010-0000-0000) 수신 4분 · 14:02"
	if err := os.WriteFile(filepath.Join(dir, "phone-calllog.txt"), []byte(digest), 0o600); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	out, err = read(context.Background(), json.RawMessage(`{"what":"calllog"}`))
	if err != nil {
		t.Fatalf("cached read: %v", err)
	}
	if !strings.Contains(out, digest) {
		t.Errorf("cached digest missing from %q", out)
	}
	// "calls" is the documented alias and must reach the same cache.
	if out2, _ := read(context.Background(), json.RawMessage(`{"what":"calls"}`)); !strings.Contains(out2, digest) {
		t.Errorf("alias \"calls\" did not serve the cache: %q", out2)
	}
}
