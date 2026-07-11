package genesis

// eprocess.go — anytime-valid sequential test primitive (RSI P1.5, PACE
// arXiv 2606.08106 + SEA 2607.00871). A betting martingale (e-process) over
// Bernoulli failure outcomes: under H0 "the failure rate is still the
// baseline p0", the wealth E is a nonnegative supermartingale with E[E]≤1,
// so by Ville's inequality P(sup E ≥ 1/α) ≤ α — the test stays valid no
// matter when you peek or stop (exactly the property the rollback watch and
// the future slow-loop promotion gate need; fixed-threshold counting like
// "3 fails in a window" has no such guarantee and mislabels P3 data).
//
// Deliberately UNWIRED in this commit: pure primitive + tests only, so the
// gates' behavior is unchanged until a follow-up threads it through the
// rollback watch and champion confirmation.

// Deterministic test/betting parameters — Go constants by the P1 invariant
// (the deterministic half of the pipeline is not self-editable).
const (
	// DefaultEProcessAlpha is the anytime-valid significance level: the
	// probability a still-healthy skill ever gets flagged is at most alpha.
	DefaultEProcessAlpha = 0.05
	// eProcessBet is the fixed betting fraction lambda. Positivity of both
	// betting factors requires lambda < 1/p0 and lambda < 1/(1-p0); with the
	// baseline clamped to [0.05, 0.95] any lambda < ~1.05 is safe. 0.5 trades
	// a little power for robustness to a misestimated baseline.
	eProcessBet = 0.5
	// Baseline clamps: a degenerate baseline (0 or 1 observed failure rate,
	// tiny pre-evolve samples) would make the martingale explode on the first
	// observation or never move. Clamping keeps the test honest-but-bounded.
	eProcessBaselineFloor = 0.05
	eProcessBaselineCeil  = 0.95
)

// EProcess accumulates evidence AGAINST the hypothesis that failures still
// occur at the baseline rate. Observe each real post-evolve use; Reject
// reports whether accumulated evidence exceeds 1/alpha. The zero value is
// not usable — construct with NewEProcess.
type EProcess struct {
	// E is the current wealth (e-value); N the number of observations.
	// Exported so the tracker can persist an in-flight test across the
	// SIGUSR1 deploy restarts.
	E float64 `json:"e"`
	N int     `json:"n"`

	Alpha    float64 `json:"alpha"`
	Baseline float64 `json:"baseline"` // clamped H0 failure rate p0
}

// NewEProcess starts a test against baselineFailRate (clamped) at alpha
// (non-positive alpha falls back to DefaultEProcessAlpha).
func NewEProcess(alpha, baselineFailRate float64) *EProcess {
	if alpha <= 0 || alpha >= 1 {
		alpha = DefaultEProcessAlpha
	}
	p0 := baselineFailRate
	if p0 < eProcessBaselineFloor {
		p0 = eProcessBaselineFloor
	}
	if p0 > eProcessBaselineCeil {
		p0 = eProcessBaselineCeil
	}
	return &EProcess{E: 1, N: 0, Alpha: alpha, Baseline: p0}
}

// Observe folds one Bernoulli outcome into the wealth: a failure multiplies
// by 1+lambda(1-p0) (evidence against H0), a success by 1-lambda*p0. Both
// factors are strictly positive under the clamps, and the expectation under
// H0 is exactly 1 — the martingale property the validity guarantee rides on.
func (p *EProcess) Observe(fail bool) {
	if p == nil {
		return
	}
	if fail {
		p.E *= 1 + eProcessBet*(1-p.Baseline)
	} else {
		p.E *= 1 - eProcessBet*p.Baseline
	}
	p.N++
}

// Reject reports whether evidence crossed the anytime-valid threshold 1/alpha.
func (p *EProcess) Reject() bool {
	return p != nil && p.E >= 1/p.Alpha
}
