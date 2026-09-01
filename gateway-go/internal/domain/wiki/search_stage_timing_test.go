package wiki

import (
	"strings"
	"testing"
	"time"
)

// A timer that is off must cost nothing and say nothing — this diagnostic lives
// in the hot path of every recall turn.
func TestStageTimerOffRecordsNothing(t *testing.T) {
	t.Setenv("DENEB_WIKI_STAGE_TIMING", "")
	timer := newStageTimer()
	timer.mark("bm25")
	timer.mark("semantic")
	if len(timer.marks) != 0 {
		t.Fatalf("off timer recorded %v", timer.marks)
	}
}

// Marks measure the span SINCE the previous mark, so an unmarked stage is
// silently added to the next one. That is not hypothetical: the first draft of
// this instrument reported rerank=141ms for a search whose reranker was nil —
// the span was carrying applyRecallTRS and the lifecycle filter, and reading it
// as "the reranker is the cost" would have been exactly wrong.
func TestStageTimerAttributesEachSpanToItsOwnMark(t *testing.T) {
	t.Setenv("DENEB_WIKI_STAGE_TIMING", "on")
	timer := newStageTimer()
	time.Sleep(15 * time.Millisecond)
	timer.mark("slow")
	timer.mark("fast")
	if len(timer.marks) != 2 {
		t.Fatalf("want 2 marks, got %v", timer.marks)
	}
	if !strings.HasPrefix(timer.marks[0], "slow=") || !strings.HasPrefix(timer.marks[1], "fast=") {
		t.Fatalf("marks not in order: %v", timer.marks)
	}
	slow := timer.marks[0]
	if slow == "slow=0ms" {
		t.Fatalf("a 15ms span read as zero: %v", timer.marks)
	}
	if timer.marks[1] != "fast=0ms" {
		t.Fatalf("the second span should be ~0, got %q — spans are not cumulative", timer.marks[1])
	}
}

// A nil timer is the batch path; it must not panic.
func TestNilStageTimerIsSafe(t *testing.T) {
	var timer *stageTimer
	timer.mark("bm25")
	timer.report("q", time.Second)
}

func TestClipRunesForLogCountsRunesNotBytes(t *testing.T) {
	// Korean queries are 3 bytes a rune; a byte clip would cut mid-character.
	got := clipRunesForLog("새만금 지체상금 협의 진행 상황 정리", 5)
	if []rune(got)[5] != '…' {
		t.Fatalf("clip did not land on rune 5: %q", got)
	}
	if same := clipRunesForLog("짧음", 5); same != "짧음" {
		t.Fatalf("short strings must pass through, got %q", same)
	}
}
