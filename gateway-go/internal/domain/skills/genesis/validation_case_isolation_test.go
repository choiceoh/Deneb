package genesis

import (
	"strings"
	"testing"
)

// A case that cannot be executed must be identifiable, or nobody can repair it.
// The 2026-08 journal carried eight executor failures across five skills with
// no way to tell which case was bad.
func TestReplayCaseLabelPrefersIDThenInput(t *testing.T) {
	tests := []struct {
		name string
		rec  SkillValidationCaseRecord
		want string
	}{{
		name: "id wins",
		rec:  SkillValidationCaseRecord{ID: "case-alpha", Replay: SkillReplayCaseRecord{Input: "무언가"}},
		want: "case-alpha",
	}, {
		name: "falls back to the input head",
		rec:  SkillValidationCaseRecord{Replay: SkillReplayCaseRecord{Input: "이 영상 요약해줘"}},
		want: "이 영상 요약해줘",
	}, {
		name: "empty input is still labelled",
		rec:  SkillValidationCaseRecord{},
		want: "(빈 입력)",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replayCaseLabel(tt.rec); got != tt.want {
				t.Fatalf("replayCaseLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

// A long input must not put a whole transcript on one log line.
func TestReplayCaseLabelBoundsLongInput(t *testing.T) {
	long := make([]rune, 400)
	for i := range long {
		long[i] = '가'
	}
	got := []rune(replayCaseLabel(SkillValidationCaseRecord{
		Replay: SkillReplayCaseRecord{Input: string(long)},
	}))
	if len(got) > 61 {
		t.Fatalf("label length = %d runes, want bounded", len(got))
	}
	if got[len(got)-1] != '…' {
		t.Errorf("truncated label must be marked, got %q", string(got[len(got)-3:]))
	}
}

// Newlines collapse — a multi-line input would otherwise break the log line
// the label exists to be greppable in.
func TestReplayCaseLabelCollapsesNewlines(t *testing.T) {
	got := replayCaseLabel(SkillValidationCaseRecord{
		Replay: SkillReplayCaseRecord{Input: "첫 줄\n둘째 줄"},
	})
	if got != "첫 줄 둘째 줄" {
		t.Fatalf("label = %q, want newlines collapsed", got)
	}
}

// The adversarial-coverage generator used to mint a placeholder replay input.
// It is not a user task, so the behavioral executor refuses it and the plan
// parse fails — 42 such records sit in the live corpus and must never reach the
// executor, while still serving the deterministic gate.
func TestReplayBehaviorEvaluableRejectsCoveragePlaceholder(t *testing.T) {
	withTool := []string{"read_spillover"}

	if replayBehaviorEvaluable(SkillReplayCaseRecord{
		Input:         "exercise the skill's use of tool read_spillover",
		RequiredTools: withTool,
	}) {
		t.Error("legacy coverage placeholder must not be behaviorally evaluable")
	}
	if replayBehaviorEvaluable(SkillReplayCaseRecord{RequiredTools: withTool}) {
		t.Error("an empty input must not be behaviorally evaluable")
	}
	// A real task with the same assertion still is — the filter must key on the
	// placeholder, not on the assertion it carries.
	if !replayBehaviorEvaluable(SkillReplayCaseRecord{
		Input:         "이 영상 요약해줘 https://youtu.be/abc123",
		RequiredTools: withTool,
	}) {
		t.Error("a real user task must stay behaviorally evaluable")
	}
	// Assertions are still required — an input alone scores nothing.
	if replayBehaviorEvaluable(SkillReplayCaseRecord{Input: "이 영상 요약해줘"}) {
		t.Error("a case with no assertions must not be evaluable")
	}
}

// The generator must borrow a real task, never synthesize one.
func TestAdversarialToolCaseBorrowsRealInput(t *testing.T) {
	body := "# S\n\n## Procedure\n\n1. read_spillover 로 넘친 자막을 읽는다.\n"
	seeded := []SkillValidationCaseRecord{{
		SkillName:          "s",
		ID:                 "seed",
		RequiredSubstrings: []string{"read_spillover"},
		Replay:             SkillReplayCaseRecord{Input: "이 영상 요약해줘 https://youtu.be/abc"},
	}}
	for _, got := range probeBehavioralCoverageGaps("s", body, seeded) {
		if got.Replay.Input != "이 영상 요약해줘 https://youtu.be/abc" {
			t.Fatalf("input = %q, want the borrowed task", got.Replay.Input)
		}
	}

	// No real task anywhere — leave Input empty rather than invent one. The
	// deterministic scorer still checks RequiredTools against the body.
	placeholderOnly := []SkillValidationCaseRecord{{
		SkillName:          "s",
		ID:                 "legacy",
		RequiredSubstrings: []string{"read_spillover"},
		Replay:             SkillReplayCaseRecord{Input: "exercise the skill's use of tool read_spillover"},
	}}
	for _, got := range probeBehavioralCoverageGaps("s", body, placeholderOnly) {
		if strings.TrimSpace(got.Replay.Input) != "" {
			t.Fatalf("input = %q, want empty when no real task exists", got.Replay.Input)
		}
		if len(got.Replay.RequiredTools) == 0 {
			t.Fatal("generated case lost its RequiredTools assertion")
		}
	}
}
