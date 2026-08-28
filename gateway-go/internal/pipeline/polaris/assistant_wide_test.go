package polaris

import (
	"strings"
	"testing"
)

// assistantWide feeds the pruner's sentence selection; these pin its three
// contracts: user rows untouched, a window inside the head is not duplicated,
// and a window beyond the head is preserved after it.
func TestAssistantWideJoinsReplyHeadWithMatchWindow(t *testing.T) {
	long := strings.Repeat("답변 문장입니다. ", 400) // ~4000 runes
	window := clipRunesHead(long, 200)

	if got := assistantWide("user", long, window); got != window {
		t.Fatalf("user rows must keep the lexical window untouched")
	}

	got := assistantWide("assistant", long, window)
	if got != clipRunesHead(long, nextTextClipRunes) {
		t.Fatalf("window inside the head must collapse to the head alone (no duplication)")
	}

	tailWindow := "머리 밖 꼬리에서만 등장하는 창"
	got = assistantWide("assistant", long, tailWindow)
	rawHead := string([]rune(long)[:100]) // clipRunesHead appends "..." — compare raw runes
	if !strings.HasPrefix(got, rawHead) || !strings.HasSuffix(got, tailWindow) {
		t.Fatalf("window beyond the head must survive after it, got %q", got[:60])
	}
}
