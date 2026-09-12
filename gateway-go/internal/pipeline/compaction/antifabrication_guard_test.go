package compaction

import (
	"strings"
	"testing"
)

// The production summarizer prompts must carry an explicit anti-fabrication
// rule — the eval candidates (prompt_eval_test.go) already had one while the
// live const lagged, and that gap let the summarizer invent figures that were
// never in the source (fabricated a 체납 amount with no origin). Guard the
// clause so the drift cannot silently return.
func TestSummarizerPrompts_RejectInventedFacts(t *testing.T) {
	for name, prompt := range map[string]string{
		"compactionSystemPrompt":   compactionSystemPrompt,
		"recompactionSystemPrompt": recompactionSystemPrompt,
	} {
		if !strings.Contains(prompt, "지어내지 마라") {
			t.Errorf("%s must forbid inventing facts not in the source", name)
		}
	}
}

// The summarizer prompts must also forbid promoting unconfirmed proposals to
// decided facts. Deneb feeds a compaction summary back as PreviousSummary on
// the next incremental update (recompaction), so a suggestion the user never
// confirmed — once recorded as a fact — accumulates across recompactions into
// a false consensus. Mirrors Goose's compaction.md "No new ideas unless user
// confirmed", scoped to Deneb's Korean fact-extraction contract. The
// recompaction path is the higher-risk surface since its job is to fold "new
// turns" into prior facts.
func TestSummarizerPrompts_RejectUnconfirmedDecisions(t *testing.T) {
	for name, prompt := range map[string]string{
		"compactionSystemPrompt":   compactionSystemPrompt,
		"recompactionSystemPrompt": recompactionSystemPrompt,
	} {
		if !strings.Contains(prompt, "사용자 미확인 결정") {
			t.Errorf("%s must forbid recording user-unconfirmed proposals as decided facts", name)
		}
		if !strings.Contains(prompt, "격상하지") {
			t.Errorf("%s must forbid elevating proposals to confirmed status", name)
		}
	}
}

// The agent-action twin of the rule above: an outcome is what the tool
// returned, not what the assistant claimed. Without it the summarizer copies
// "회신 메일을 보냈습니다" into "[확실] 회신 메일 발송" even when the tool
// result two lines earlier was an error, and a promised-but-never-called
// action becomes a completed one — a false completion that the next
// recompaction then treats as a settled fact. Both prompts must carry the
// contract (the recompaction path is where "열린 루프를 완료로 옮겨라" is most
// tempted to close a loop on the assistant's word); the live measurement is
// prompt_eval_action_outcome_test.go.
func TestSummarizerPrompts_RecordActionOutcomesAsToolReturns(t *testing.T) {
	for name, prompt := range map[string]string{
		"compactionSystemPrompt":   compactionSystemPrompt,
		"recompactionSystemPrompt": recompactionSystemPrompt,
	} {
		for _, anchor := range []string{"행위 결과 중립", "도구 반환값이 이긴다", "도구 호출 없이"} {
			if !strings.Contains(prompt, anchor) {
				t.Errorf("%s must carry the action-outcome neutrality contract (missing %q)", name, anchor)
			}
		}
	}
	if !strings.Contains(compactionOutputFormat, "오류 반환도 반환값") {
		t.Error("the shared skeleton must tell the model an error return is a tool outcome to record verbatim")
	}
}

// The untrusted fence must warn the model that figures inside a summary are
// unverified and must not be asserted to the user as fact.
func TestContextFence_EmitsUnverifiedWarning(t *testing.T) {
	out := FormatContextFence("polaris", "conversation-summary", "제목", "본문")
	for _, want := range []string{"UNVERIFIED", "established fact"} {
		if !strings.Contains(out, want) {
			t.Errorf("fence must warn that summary figures are unverified (missing %q)", want)
		}
	}
}
