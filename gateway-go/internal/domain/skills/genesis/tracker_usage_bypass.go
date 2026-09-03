package genesis

// tracker_usage_bypass.go — the success-side blind spot in skill evidence.
//
// SkillFailureLayers locates where a FAILED run broke, and deliberately ignores
// successes: a success carries no failure to locate. That leaves one case
// unmeasured, and it is the case that can never correct itself.
//
// A skill can be delivered into a turn, have none of its declared procedure
// run, and the turn still succeeds. Today that record is a plain success: it
// lifts the skill's success rate, and evolution only ever fires on
// UNDERPERFORMERS. So a skill whose instructions the model routinely walks past
// looks healthiest at exactly the moment it stopped contributing anything, and
// nothing in the loop is pointed at it. The idle curator cannot reach it either
// — it is not idle, it is being delivered every time.
//
// EvoHarness-RL (arXiv 2608.05446) hits the same failure mode from the agent
// side and fixes it with a write rule: when recalled experience turns out to be
// empty or contradicted by what actually worked, the agent MUST write the
// correction back — on SUCCESS, not only on failure. Their worked example is a
// skill entry claiming the kettle is on the countertop; the agent finds it on a
// stoveburner, succeeds, and the stale entry would have survived untouched
// without that rule. This file is the measurement half of that rule for Deneb:
// it counts the successes in which a delivered skill was bypassed, so a stale
// skill has a signal at all.
//
// ADVISORY ONLY, on purpose, and it must stay that way until the counts have
// production history. Nothing here feeds the evolver's success-rate gate.
// Re-weighting a gate on a fresh signal is how evolve thrash starts (PR #2328),
// and the source paper made the mirror-image mistake: it reported harness usage
// collapsing to near zero as a virtue without ever re-running the task with the
// harness removed, so it never learned whether the thing it stopped calling
// still mattered. Counting first is the cheap half; deciding what a high bypass
// rate MEANS (stale body, wrong trigger, or a skill that was always redundant)
// needs the counts to exist.

const (
	// Evidence floor for the bypass advisory. A single bypassed success is
	// noise: a documented branch, a turn the model solved before reaching the
	// skill body, a cached answer. The signal is a skill that keeps being
	// delivered and keeps being walked past.
	defaultSkillBypassMinAttributed = 4
	// Rate floor, in percent of attributed successes. At half the successful
	// runs the skill is no longer incidental to the outcome — it is decorative.
	defaultSkillBypassRatePct = 50
)

// SkillBypassSignal splits SUCCESSFUL counted real-use runs by whether the
// skill's declared procedure actually ran. It is the success-side counterpart
// to SkillFailureLayers and shares its attribution vocabulary (ADR 0006):
// Exercised comes from declared tools or declared output evidence, so a skill
// that declares neither is unmeasurable here rather than innocent.
type SkillBypassSignal struct {
	// ExercisedSuccesses succeeded with the declared procedure observed — the
	// skill plausibly contributed to the outcome.
	ExercisedSuccesses int `json:"exercisedSuccesses"`
	// BypassedSuccesses succeeded with NONE of the declared procedure
	// observed: delivered, walked past, and the turn worked anyway.
	BypassedSuccesses int `json:"bypassedSuccesses"`
	// UnattributableSuccesses come from skills declaring no exercise evidence
	// (or records written before attribution existed) — unknown, not innocent.
	UnattributableSuccesses int `json:"unattributableSuccesses"`
	// AutoLoadBypasses / ModelReadBypasses split the bypasses by how the skill
	// reached the turn. The two mean different repairs: an auto-loaded skill
	// that is walked past is a trigger problem (it is being pushed into turns
	// it does not serve), while one the model chose to read and then ignored is
	// a body problem (it went looking and found nothing usable).
	AutoLoadBypasses  int `json:"autoLoadBypasses,omitempty"`
	ModelReadBypasses int `json:"modelReadBypasses,omitempty"`
}

// observe folds one counted record into the bypass split. Failures carry their
// own split (SkillFailureLayers.observe); only successes are counted here, so
// the two are exact complements over the same counted-record set.
func (b *SkillBypassSignal) observe(r UsageRecord) {
	if !r.Success {
		return
	}
	switch r.Exercised {
	case UsageExercisedYes:
		b.ExercisedSuccesses++
	case UsageExercisedNo:
		b.BypassedSuccesses++
		switch r.Delivery {
		case UsageDeliveryAutoLoad:
			b.AutoLoadBypasses++
		case UsageDeliveryModelRead:
			b.ModelReadBypasses++
		}
	default:
		b.UnattributableSuccesses++
	}
}

// AttributedSuccesses is the only honest denominator for the rate: successes
// whose exercise status is actually known. Unattributable records are excluded
// rather than counted as exercised, which would hide every legacy skill.
func (b SkillBypassSignal) AttributedSuccesses() int {
	return b.ExercisedSuccesses + b.BypassedSuccesses
}

// BypassRate is the share of attributed successes in which the skill was
// delivered and then walked past. Zero when there is no attributed evidence —
// callers must check AttributedSuccesses before reading a rate as meaningful.
func (b SkillBypassSignal) BypassRate() float64 {
	total := b.AttributedSuccesses()
	if total == 0 {
		return 0
	}
	return float64(b.BypassedSuccesses) / float64(total)
}

// Actionable reports whether the bypass evidence clears both floors and is
// worth an operator's attention. Integer comparison keeps the verdict exact at
// the boundary; a float rate would make 2/4 depend on rounding.
func (b SkillBypassSignal) Actionable() bool {
	attributed := b.AttributedSuccesses()
	if attributed < defaultSkillBypassMinAttributed {
		return false
	}
	return b.BypassedSuccesses*100 >= attributed*defaultSkillBypassRatePct
}
