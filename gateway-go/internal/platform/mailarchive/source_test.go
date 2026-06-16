package mailarchive

import (
	"strconv"
	"strings"
	"testing"
)

func seq(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = prefix + strconv.Itoa(i+1)
	}
	return out
}

func TestPickRelatedUIDs(t *testing.T) {
	const maxThread, maxSender, maxFetch = 10, 5, 18

	t.Run("thread ancestors survive a chatty sender", func(t *testing.T) {
		thread := []string{"t1", "t2"}
		sender := seq("s", 30) // far more than maxSender
		got := pickRelatedUIDs(thread, sender, maxThread, maxSender, maxFetch)
		// Thread ancestors must be present (the bug dropped them).
		joined := strings.Join(got, ",")
		if !strings.Contains(joined, "t1") || !strings.Contains(joined, "t2") {
			t.Fatalf("thread ancestors dropped: %v", got)
		}
		// And appear at the front.
		if got[0] != "t1" || got[1] != "t2" {
			t.Fatalf("thread ancestors not front: %v", got)
		}
		// Sender capped at maxSender, keeping the most-recent (highest).
		if len(got) != 2+maxSender {
			t.Fatalf("len=%d want %d", len(got), 2+maxSender)
		}
		if !strings.Contains(joined, "s30") || strings.Contains(joined, "s1,") {
			t.Fatalf("sender cap should keep most-recent: %v", got)
		}
	})

	t.Run("thread capped head-first", func(t *testing.T) {
		got := pickRelatedUIDs(seq("t", 14), nil, maxThread, maxSender, maxFetch)
		if len(got) != maxThread {
			t.Fatalf("len=%d want %d", len(got), maxThread)
		}
		if got[0] != "t1" || got[maxThread-1] != "t10" {
			t.Fatalf("thread should keep front (t1..t10): %v", got)
		}
	})

	t.Run("dedup across thread and sender keeps thread position", func(t *testing.T) {
		got := pickRelatedUIDs([]string{"a", "b"}, []string{"b", "c"}, maxThread, maxSender, maxFetch)
		if strings.Join(got, ",") != "a,b,c" {
			t.Fatalf("got %v want [a b c]", got)
		}
	})

	t.Run("maxFetch keeps the front", func(t *testing.T) {
		got := pickRelatedUIDs(seq("t", 10), seq("s", 10), maxThread, maxSender, 8)
		if len(got) != 8 || got[0] != "t1" {
			t.Fatalf("maxFetch should head-cap: len=%d first=%s", len(got), got[0])
		}
	})
}
