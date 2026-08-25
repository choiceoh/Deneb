package genesis

import "testing"

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
