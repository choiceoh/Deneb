package modeltuner

import (
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// adaptiveEffortTuneEnv is the opt-in flag for the adaptive effort-router nudge.
// Off by default: the prod scorecard already noted "escalation 0%, sample too
// small", so single-user traffic isn't ready to auto-tune the gate yet — but the
// loop is wired and ready to switch on.
const adaptiveEffortTuneEnv = "DENEB_ADAPTIVE_EFFORT_TUNE"

// adaptiveEffortEnabled reports whether the effort nudge is switched on.
func adaptiveEffortEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(adaptiveEffortTuneEnv))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// applyEffortNudge closes the "telemetry computes the recalibration verdict but
// nothing adjusts the router" loop: it reads the per-model effort scorecard for
// the window, runs the bounded router.EffortNudge policy on each model's
// MaxSimpleRunes gate, and writes the result back through
// modelrole.Registry.SetTunedMaxSimpleRunes — the runtime-writable gate the live
// request path resolves via RoutingProfileForModel.
//
// It mirrors SetTunedMaxTokens exactly: a periodic step that re-derives a
// runtime knob from accumulated stats within a tight band, opt-in, and logged.
// No-op without the env flag or a registry. Returns the count of models nudged.
//
// The current gate value is read back from the registry's own resolution so the
// nudge composes across cycles (each cycle steps from the last cycle's tuned
// value, not from the static default); a model with no tuned value yet starts
// from its resolved profile gate.
func (t *Task) applyEffortNudge(stats map[string]agentlog.EffortStat) int {
	if t.deps.Registry == nil || !adaptiveEffortEnabled() {
		return 0
	}
	nudged := 0
	for model, s := range stats {
		if model == "" {
			continue // runs with no recorded model can't be attributed to a gate
		}
		current := t.currentSimpleRunes(model)
		if current <= 0 {
			continue // routing inert for this model (no toggle); nothing to tune
		}
		next, changed := router.EffortNudge(current, effortSignal(s))
		if !changed {
			continue
		}
		t.deps.Registry.SetTunedMaxSimpleRunes(model, next)
		nudged++
		t.deps.Logger.Info("modeltuner: effort gate nudged",
			"model", model,
			"maxSimpleRunes", current, "to", next,
			"escalationRate", s.EscalationRate(), "routedShare", s.RoutedShare(),
			"sample", s.RoutedRuns+s.KeptRuns, "routedRuns", s.RoutedRuns)
	}
	return nudged
}

// currentSimpleRunes resolves the gate value the next nudge should step from:
// the model's live MaxSimpleRunes after the registry layers profile + override +
// any prior tuned value. The model name alone resolves the routing profile here
// (provider doesn't change the MaxSimpleRunes gate in DefaultProfile), so the
// effort scorecard's model key suffices.
func (t *Task) currentSimpleRunes(model string) int {
	return t.deps.Registry.RoutingProfileForModel("", model).MaxSimpleRunes
}

// effortSignal projects an agentlog.EffortStat onto the narrow scalar input the
// pure router.EffortNudge policy acts on.
func effortSignal(s agentlog.EffortStat) router.EffortSignal {
	return router.EffortSignal{
		Sample:         s.RoutedRuns + s.KeptRuns,
		RoutedRuns:     s.RoutedRuns,
		EscalationRate: s.EscalationRate(),
		RoutedShare:    s.RoutedShare(),
	}
}
