// Package regressionwatch is Stage 1 of the autoresearch cold-start trigger:
// an observe-only background task that samples operational telemetry from
// several runtime surfaces, compares each signal against a persisted rolling
// baseline, and logs detected regressions.
//
// It deliberately does NOT create optimization goals yet. Autonomous
// self-improvement at Deneb ships observe-only first (the self-evolution loop
// died silently several times before that discipline; see the project memory),
// so the goal-enqueue path is deferred until these thresholds are validated
// against real traffic. The eventual cold start is: regression detected here →
// confirmed by a dev synthetic bench → an `optimize` goal enqueued into the
// goal loop (.claude/rules/optimization.md).
//
// Design: regression signals come from many surfaces (agent logs, the
// model-health circuit, vLLM cache, delivery failures, session lifecycle), so
// each surface is a SignalSource adapter and the watcher core is
// source-agnostic. New surfaces plug in without touching detection or
// persistence.
package regressionwatch

// Signal is one normalized metric sample from an observation surface. Value is
// a rate (0..1), a latency (ms), or a count — only required to be comparable
// across cycles for the same Key/Scope. HigherWorse fixes the regression
// direction so the detector needs no per-key knowledge: an error rate rising is
// bad (HigherWorse=true); a cache hit rate falling is bad (HigherWorse=false).
type Signal struct {
	Key         string  // stable id, e.g. "agentlog.error_rate"
	Scope       string  // sub-scope (model name, channel); "" for a global signal
	Value       float64 // normalized value
	Sample      int     // sample size behind Value (noise gate in detect)
	HigherWorse bool    // true: a rise is a regression; false: a fall is
	// Kind selects how detect judges drift (see SignalKind).
	Kind SignalKind
	// HardFloor is the absolute rise that trips a KindCount signal (ignored for
	// other kinds). 0 falls back to Thresholds.CountHard; a binary 0/1 signal (a
	// circuit flipping open) sets it to 1.
	HardFloor float64
}

// SignalKind selects the regression test for a signal, because signals come in
// three shapes one threshold can't fairly judge.
type SignalKind int

const (
	// KindRate is a 0..1 rate (error rate, cache hit rate): trips on a relative
	// move above the noise floor OR a hard absolute move.
	KindRate SignalKind = iota
	// KindScalar is an unbounded magnitude (latency ms, a mean): an absolute
	// floor in "points" is meaningless, so it trips on relative change alone.
	KindScalar
	// KindCount is an event count whose healthy baseline is ~0 (delivery
	// failures, a circuit flipping open): a relative test can't fire from a zero
	// baseline, so it trips when the count rises past an absolute HardFloor.
	KindCount
)

// FullKey joins Key and Scope into the stable baseline-map key.
func (s Signal) FullKey() string {
	if s.Scope == "" {
		return s.Key
	}
	return s.Key + "@" + s.Scope
}

// SignalSource is one observation surface contributing regression signals.
// Sample is called once per watch cycle and must be cheap and non-blocking —
// it runs on the autonomous task goroutine. A source that needs the network
// (e.g. scraping an engine's /metrics) should bound its own timeout.
type SignalSource interface {
	Name() string
	Sample() []Signal
}
