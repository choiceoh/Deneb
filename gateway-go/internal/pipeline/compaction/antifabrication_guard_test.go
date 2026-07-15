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
