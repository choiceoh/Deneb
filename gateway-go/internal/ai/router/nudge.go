package router

// The effort router's MaxSimpleRunes gate ships as a static DSV4-Flash-tuned
// constant (DefaultProfile, 140). The agent-log effort scorecard already
// computes the recalibration verdict from real traffic — the escalation rate
// (the router's false-route-off rate) and the routed share (how often it fires)
// — and its doc states it verbatim: "a high rate says the gates should be
// stricter; a near-zero RoutedShare says they are too strict." But nothing
// acted on it.
//
// EffortNudge closes that loop: a pure, bounded one-step recalibration of
// MaxSimpleRunes from those two signals, mirroring the modeltuner precedent (a
// periodic task reads accumulated stats and nudges a runtime-writable knob
// within tight bounds). It stays in this package because it IS routing policy,
// and pure (no env, no I/O, no clock, no stats-package coupling — it takes the
// scalars the caller already computed) so the bounds/clamp logic is
// unit-testable in isolation; the periodic driver, the per-model aggregation,
// and the env opt-in live in modeltuner.
//
// The error asymmetry the router is built on holds here too: a false-easy
// (routing off when thinking was needed) costs answer quality, a false-hard
// only wastes tokens. So the policy is conservative — it requires a real sample
// before moving, steps in small increments, and clamps hard to a sane band so a
// runaway feedback loop can't strand the gate at 0 or ∞.
const (
	// EffortNudgeMinSample is the minimum number of router-active runs (routed +
	// kept) required before the gate moves at all. Single-user traffic produces
	// small samples (the prod scorecard noted "escalation 0%, sample too small"),
	// so a few runs of noise must never trip a nudge.
	EffortNudgeMinSample = 30

	// EffortNudgeMinRouted is the minimum routed-off runs required before the
	// escalation-rate signal is trusted — an escalation fraction over 2-3 routed
	// runs is noise, not a false-route-off problem.
	EffortNudgeMinRouted = 10

	// EffortNudgeHighEscalation: above this false-route-off rate the gate is too
	// aggressive (routing off when thinking was needed) and steps DOWN (stricter
	// — fewer turns qualify as simple). Matches the observe.go scorecard's
	// "gates may be too aggressive" threshold (0.15) so the printed verdict and
	// the action agree.
	EffortNudgeHighEscalation = 0.15

	// EffortNudgeLowRoutedShare: at or below this routed share the gate is too
	// strict (the router rarely fires) and steps UP (looser) — but only when
	// escalation is healthy (EffortNudgeHealthyEscalation), so we never widen a
	// gate that is already routing off too eagerly.
	EffortNudgeLowRoutedShare = 0.05

	// EffortNudgeHealthyEscalation gates the widen direction: routed share may
	// only be raised when the false-route-off rate is at or below this, so a
	// near-zero routed share that coexists with high escalation (a tiny, wrong
	// router) is left alone rather than made tinier or wider.
	EffortNudgeHealthyEscalation = 0.05

	// EffortNudgeStep is the per-cycle adjustment to MaxSimpleRunes (in runes).
	// Small on purpose: a 6h cycle moving 10 runes converges over days, not
	// minutes, which is the right pace for a single-user feedback signal.
	EffortNudgeStep = 10

	// EffortNudgeMin / EffortNudgeMax clamp the gate to a sane band so the loop
	// can never run away. 60 keeps the gate from collapsing so far that ordinary
	// short turns all keep thinking; 220 keeps it from widening so far that long
	// substantive turns get routed off. The shipped default (140) sits mid-band.
	EffortNudgeMin = 60
	EffortNudgeMax = 220
)

// EffortSignal is the narrow recalibration input EffortNudge acts on — the two
// gate-health rates the effort scorecard already computes, plus the sample
// sizes that say whether to trust them. The caller builds it from an
// agentlog.EffortStat (RoutedShare(), EscalationRate(), RoutedRuns,
// RoutedRuns+KeptRuns); keeping the policy on plain scalars preserves the
// package's purity (no stats-package import) and keeps the bounds logic trivial
// to unit-test.
type EffortSignal struct {
	// Sample is the total router-active run count (routed + kept) — the gate
	// guard against nudging on noise.
	Sample int
	// RoutedRuns is how many of those routed off — the denominator the
	// escalation rate is trusted over.
	RoutedRuns int
	// EscalationRate is the fraction of routed-off runs that had to restore
	// thinking (the router's false-route-off rate).
	EscalationRate float64
	// RoutedShare is the fraction of router-active runs that routed off (how
	// often the router fires).
	RoutedShare float64
}

// EffortNudge computes a one-step, bounded recalibration of a model's
// MaxSimpleRunes gate from its accumulated effort signal. It returns the new
// gate value and whether it changed from current.
//
// Direction (at most one fires per call):
//   - EscalationRate > EffortNudgeHighEscalation (over EffortNudgeMinRouted
//     routed runs): the gate is too aggressive → step DOWN (stricter).
//   - RoutedShare <= EffortNudgeLowRoutedShare AND EscalationRate <=
//     EffortNudgeHealthyEscalation: the gate is too strict → step UP (looser).
//
// It is a no-op (returns current, false) when:
//   - current <= 0 (an uninitialized/sentinel gate is left for the caller to
//     seed from the profile),
//   - the total sample is below EffortNudgeMinSample (don't nudge on noise),
//   - neither direction's condition holds,
//   - the computed step would leave the value unchanged after clamping (already
//     at the band edge in the nudged direction).
//
// The result is always within [EffortNudgeMin, EffortNudgeMax]; a current value
// already outside the band is pulled back toward it by the clamp.
func EffortNudge(current int, sig EffortSignal) (int, bool) {
	if current <= 0 {
		return current, false
	}
	if sig.Sample < EffortNudgeMinSample {
		return current, false
	}

	next := current
	switch {
	case sig.RoutedRuns >= EffortNudgeMinRouted && sig.EscalationRate > EffortNudgeHighEscalation:
		// Too aggressive: tighten the gate so fewer turns route off.
		next = current - EffortNudgeStep
	case sig.RoutedShare <= EffortNudgeLowRoutedShare && sig.EscalationRate <= EffortNudgeHealthyEscalation:
		// Too strict: widen the gate so more turns qualify as simple. The
		// healthy-escalation guard prevents widening a gate that is already
		// routing off too eagerly (which would also show a low routed share when
		// most traffic is kept).
		next = current + EffortNudgeStep
	default:
		return current, false
	}

	next = clampRunes(next)
	if next == current {
		return current, false
	}
	return next, true
}

// clampRunes pins v to the [EffortNudgeMin, EffortNudgeMax] band.
func clampRunes(v int) int {
	if v < EffortNudgeMin {
		return EffortNudgeMin
	}
	if v > EffortNudgeMax {
		return EffortNudgeMax
	}
	return v
}
