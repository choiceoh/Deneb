package proactive

import "testing"

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
