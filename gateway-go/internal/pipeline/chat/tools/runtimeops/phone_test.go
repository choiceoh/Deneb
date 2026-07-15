package runtimeops

import (
	"context"
	"encoding/json"
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
		"calllog":   "지원이 종료",
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
