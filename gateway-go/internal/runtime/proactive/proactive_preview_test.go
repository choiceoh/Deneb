package proactive

import "testing"

func TestPushPreviewFirstLineTruncatesLongInput(t *testing.T) {
	if got := pushPreview("  첫 줄\n둘째 줄\n셋째"); got != "첫 줄" {
		t.Fatalf("pushPreview first-line = %q", got)
	}
	long := ""
	for i := 0; i < 200; i++ {
		long += "가"
	}
	got := pushPreview(long)
	if []rune(got)[len([]rune(got))-1] != '…' {
		t.Fatalf("pushPreview should ellipsize long input")
	}
	if n := len([]rune(got)); n != 141 { // 140 + ellipsis
		t.Fatalf("pushPreview rune len = %d, want 141", n)
	}
}
