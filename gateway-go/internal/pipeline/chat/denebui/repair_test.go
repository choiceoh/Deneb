package denebui

import (
	"strings"
	"testing"
)

// Split opener outside any fence: the whole card sits inside an info-less code
// block ("```" / "deneb-ui" / markup) and would render as raw markup. Observed
// in production (2026-07-09 approval card).
func TestRepairFenceGlitches_SplitOpener(t *testing.T) {
	in := strings.Join([]string{
		"판단 근거를 정리했습니다.",
		"",
		"```",
		"deneb-ui",
		"<column>",
		`<alert severity="warning" title="출장 결재">결재 대기</alert>`,
		"</column>",
		"```",
		"",
		"참고: 규모 차이는 현장에서 확인 요망.",
	}, "\n")

	got, repaired := RepairFenceGlitches(in)
	if !repaired {
		t.Fatal("repaired = false, want true")
	}
	fences := ExtractFences(got)
	if len(fences) != 1 {
		t.Fatalf("ExtractFences after repair = %d fences, want 1\n%s", len(fences), got)
	}
	if issues, err := Validate(fences[0]); err != nil || len(issues) != 0 {
		t.Fatalf("repaired card invalid: issues=%v err=%v", issues, err)
	}
	if !strings.Contains(got, "판단 근거를 정리했습니다.") || !strings.Contains(got, "참고: 규모 차이는 현장에서 확인 요망.") {
		t.Fatalf("surrounding prose lost:\n%s", got)
	}
	if strings.Contains(got, "\ndeneb-ui\n") {
		t.Fatalf("orphaned info line survived:\n%s", got)
	}
}

// Restart glitch inside an open card fence: the model closes the card
// mid-stream ("```" + orphaned "deneb-ui" line) and rewrites the whole card.
// The aborted truncated attempt must be dropped and the restarted card kept.
// Observed in production (2026-07-20 approval card).
func TestRepairFenceGlitches_RestartDropsAbortedAttempt(t *testing.T) {
	in := strings.Join([]string{
		"```deneb-ui",
		"<column>",
		"<card>",
		`<text style="headline">발주의 건</text>`,
		`<text style="`, // truncated mid-attribute — the aborted attempt
		"```",
		"deneb-ui",
		"<column>",
		"<card>",
		`<text style="headline">발주의 건</text>`,
		`<text style="body">재작성된 본문</text>`,
		"</card>",
		"</column>",
		"```",
		"",
		"k3",
	}, "\n")

	got, repaired := RepairFenceGlitches(in)
	if !repaired {
		t.Fatal("repaired = false, want true")
	}
	fences := ExtractFences(got)
	if len(fences) != 1 {
		t.Fatalf("ExtractFences after repair = %d fences, want 1\n%s", len(fences), got)
	}
	if strings.Contains(fences[0], `<text style="`+"\n") || strings.Count(got, "headline") != 1 {
		t.Fatalf("aborted attempt not dropped:\n%s", got)
	}
	if !strings.Contains(fences[0], "재작성된 본문") {
		t.Fatalf("restarted card lost:\n%s", got)
	}
	if issues, err := Validate(fences[0]); err != nil || len(issues) != 0 {
		t.Fatalf("repaired card invalid: issues=%v err=%v", issues, err)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "k3") {
		t.Fatalf("trailing model footer lost:\n%s", got)
	}
}

// Two complete sequential cards are a legitimate delivery, not a glitch.
func TestRepairFenceGlitches_TwoCompleteCardsUntouched(t *testing.T) {
	in := strings.Join([]string{
		"```deneb-ui",
		"<column><text>본문</text></column>",
		"```",
		"",
		"```deneb-ui",
		"<column><text>참고</text></column>",
		"```",
	}, "\n")
	got, repaired := RepairFenceGlitches(in)
	if repaired || got != in {
		t.Fatalf("legit multi-card body was modified:\n%s", got)
	}
}

// A generic code fence whose content happens to contain a "deneb-ui" line must
// stay untouched — only an info-less fence STARTING with the orphaned info
// line is the glitch.
func TestRepairFenceGlitches_GenericFenceContentUntouched(t *testing.T) {
	in := strings.Join([]string{
		"```go",
		`const FenceInfo = "deneb-ui"`,
		"```",
		"",
		"```",
		"plain block",
		"deneb-ui",
		"```",
	}, "\n")
	got, repaired := RepairFenceGlitches(in)
	if repaired || got != in {
		t.Fatalf("generic fence content was modified:\n%s", got)
	}
}

func TestRepairFenceGlitches_NoFenceUnchanged(t *testing.T) {
	in := "펜스 없는 평문 보고입니다.\ndeneb-ui 라는 단어만 있는 줄도 안전해야 한다."
	if got, repaired := RepairFenceGlitches(in); repaired || got != in {
		t.Fatalf("plain text was modified:\n%s", got)
	}
}

// A restart whose aborted opener carried glued prose keeps that prose.
func TestRepairFenceGlitches_RestartKeepsOpenerProsePrefix(t *testing.T) {
	in := strings.Join([]string{
		"정리했습니다.```deneb-ui",
		"<column><text>부분</text>",
		"```",
		"deneb-ui",
		"<column><text>전체 재작성</text></column>",
		"```",
	}, "\n")
	got, repaired := RepairFenceGlitches(in)
	if !repaired {
		t.Fatal("repaired = false, want true")
	}
	if !strings.Contains(got, "정리했습니다.") {
		t.Fatalf("glued prose prefix lost:\n%s", got)
	}
	fences := ExtractFences(got)
	if len(fences) != 1 || !strings.Contains(fences[0], "전체 재작성") || strings.Contains(fences[0], "부분") {
		t.Fatalf("restart not applied cleanly: %q", fences)
	}
}

// NormalizeFinalReply must deliver a repaired card instead of raw markup.
func TestNormalizeFinalReply_RepairsSplitOpener(t *testing.T) {
	in := strings.Join([]string{
		"```",
		"deneb-ui",
		"<column><text>본문</text></column>",
		"```",
	}, "\n")
	got := NormalizeFinalReply(in, "test-session", nil)
	if !strings.HasPrefix(got, "```"+FenceInfo+"\n") {
		t.Fatalf("split opener not repaired into a card:\n%s", got)
	}
	if fences := ExtractFences(got); len(fences) != 1 {
		t.Fatalf("want 1 card fence, got %d:\n%s", len(fences), got)
	}
}
