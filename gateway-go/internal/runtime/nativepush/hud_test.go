package nativepush

import (
	"strings"
	"testing"
)

func TestFormatHUDPushOneLine(t *testing.T) {
	ev := FormatHUDPush(Event{
		Title: "Deneb",
		Body:  "## 긴급\n**공문 회신** 필요 https://example.com/x\n둘째 줄",
		Kind:  PushKindWorkfeed,
	})
	if ev.Title != "Deneb · 업무" {
		t.Fatalf("title=%q", ev.Title)
	}
	for _, bad := range []string{"**", "https://", "\n", "##"} {
		if strings.Contains(ev.Body, bad) {
			t.Fatalf("body not flattened (%q): %q", bad, ev.Body)
		}
	}
	if !strings.Contains(ev.Body, "공문") {
		t.Fatalf("lost content: %q", ev.Body)
	}
}

func TestFormatHUDPushSkipsCommandFrames(t *testing.T) {
	in := Event{Title: "cmd", Body: "x", Kind: PushKindPhoneAction, Data: map[string]string{"a": "1"}}
	out := FormatHUDPush(in)
	if out.Title != "cmd" || out.Body != "x" {
		t.Fatalf("command frame mutated: %+v", out)
	}
}

func TestFormatHUDPushKeepsFleetTitle(t *testing.T) {
	ev := FormatHUDPush(Event{
		Title: "🔴 플릿 · node down: srv3",
		Body:  "ssh unreachable",
		Kind:  PushKindFleet,
	})
	t.Logf("title=%q body=%q", ev.Title, ev.Body)
	if ev.Title != "🔴 플릿 · node down: srv3" {
		t.Fatalf("title mutated/truncated: %q", ev.Title)
	}
	if ev.Body != "ssh unreachable" {
		t.Fatalf("body=%q", ev.Body)
	}
}
