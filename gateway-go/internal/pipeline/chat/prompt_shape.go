package chat

// Prompt-plane observability — what actually reached the model, and what the
// budget silently refused on the way.
//
// Every other plane of the gateway has a watcher: the error ring feeds
// RuntimeErrorMiningTask, the improvement loops feed the observatory digest,
// and a lane that goes quiet trips lanewatch. The prompt plane — the single
// largest determinant of answer quality — had none. Its only audit is
// prompt_audit_test.go, a build-tagged harness an operator runs by hand; the
// one time that was done (2026-06-12) it immediately found AGENTS.md silently
// head/tail-truncated on EVERY turn, for an unknown number of weeks.
//
// That is the failure mode this file addresses, and it is structural rather
// than accidental: finalizePrompt drops the tier-1 memory block whenever the
// static prompt has already eaten the budget, and promptbudget.Optimize halves
// a priority-2 fragment under pressure. Both are correct behavior — the prompt
// MUST fit. Both are also completely silent, so a gateway serving answers with
// no memory block looks, in the journal, exactly like one serving answers with
// a full memory block.
//
// Sizes were already recorded (agentlog RunPrepData). What was missing is the
// DIFFERENCE between what assembly asked for and what survived. Only that
// difference distinguishes "tier-1 was empty this turn" from "tier-1 was built
// and then thrown away".

import "log/slog"

// promptBudgetOutcome reports what finalizePrompt did to the variable prompt
// additions. The zero value means "nothing was requested", which is why every
// field is phrased against a requested block rather than as a bare count — a
// dropped block and an absent block are the pair this type exists to separate.
type promptBudgetOutcome struct {
	// Tier1RequestedTokens is the tier-1 memory block as assembly built it,
	// before the budget had an opinion. Zero when no block was built.
	Tier1RequestedTokens int
	// Tier1AdmittedTokens is what actually reached the system prompt.
	Tier1AdmittedTokens int
	// BaseTokens is the static prompt the addition had to fit alongside, and
	// BudgetTokens the ceiling both share. BaseTokens >= BudgetTokens is the
	// signature of a static prompt that has grown past its own allowance —
	// the condition under which NO variable addition can ever be admitted.
	BaseTokens   int
	BudgetTokens int
}

// tier1Dropped reports a tier-1 block that was built and then admitted at zero.
func (o promptBudgetOutcome) tier1Dropped() bool {
	return o.Tier1RequestedTokens > 0 && o.Tier1AdmittedTokens == 0
}

// tier1Shrunk reports a tier-1 block that survived, but not intact.
func (o promptBudgetOutcome) tier1Shrunk() bool {
	return o.Tier1AdmittedTokens > 0 && o.Tier1AdmittedTokens < o.Tier1RequestedTokens
}

// starvedByStaticPrompt reports the structural case, as distinct from a single
// oversized addition: the static prompt alone meets or exceeds the budget, so
// the shortfall is not this turn's memory block being large — it is that there
// was never any room. This is the reading that should send someone to the
// static prompt rather than to the wiki.
func (o promptBudgetOutcome) starvedByStaticPrompt() bool {
	return o.BudgetTokens > 0 && o.BaseTokens >= o.BudgetTokens
}

// logPromptShape emits the assembly-vs-survival difference, and only that.
//
// Deliberately Warn, not Info: an intact prompt is already covered by the
// existing "system prompt finalized" line and by agentlog's sizes, so a
// per-turn Info here would be pure volume. A block that was built and then
// discarded is a degraded answer the operator never sees a symptom of, and it
// belongs at the level the error ring actually retains.
func logPromptShape(logger *slog.Logger, o promptBudgetOutcome, sessionKey string) {
	if logger == nil {
		return
	}
	if !o.tier1Dropped() && !o.tier1Shrunk() {
		return
	}
	verdict := "축소"
	if o.tier1Dropped() {
		verdict = "누락"
	}
	// The static-prompt reading rides along because it names the culprit: the
	// same drop means different things depending on whether the budget had any
	// headroom at all.
	cause := "이번 턴 메모리 블록이 남은 예산보다 큼"
	if o.starvedByStaticPrompt() {
		cause = "정적 프롬프트가 예산을 이미 소진 — 어떤 가변 추가도 들어갈 수 없음"
	}
	logger.Warn("prompt-plane: tier-1 메모리 블록 "+verdict,
		"requestedTokens", o.Tier1RequestedTokens,
		"admittedTokens", o.Tier1AdmittedTokens,
		"baseTokens", o.BaseTokens,
		"budgetTokens", o.BudgetTokens,
		"cause", cause,
		"sessionKey", sessionKey)
}
