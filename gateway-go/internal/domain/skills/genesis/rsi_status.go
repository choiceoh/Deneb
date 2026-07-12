package genesis

// RSI loop-status computation — the in-process companion to
// scripts/audit/rsi_status.py, serving the native + andromeda "recursive
// self-improvement" viewers over the miniapp.rsi.status RPC. The Python audit
// script re-parses JSONL; this composes the tracker's live 7-day aggregates.
//
// The four layers and their honest states mirror the audit script exactly:
//
//	LIVE        producing AND consuming — the loop is turning.
//	DATA-GATED  built and running, waiting for fuel to accumulate (NOT a defect).
//	STARVED     built, but its input source is empty (a wiring gap).
//	FROZEN      the drift self-brake halted auto-adoption.
//	IDLE        no recent activity.
//
// The DATA-GATED vs STARVED distinction is the whole point: the first is the
// correct state of a young loop with no data yet; the second is an actionable
// gap. A naive "0 events" count conflates them.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RSI layer states.
const (
	RSIStateLive      = "LIVE"
	RSIStateDataGated = "DATA-GATED"
	RSIStateStarved   = "STARVED"
	RSIStateFrozen    = "FROZEN"
	RSIStateIdle      = "IDLE"
)

// rsiSubtleDegradationClasses are the judge-degradation classes that actually
// produce labeled misses (P3 fuel); a ledger with only blatant classes is
// data-gated, not broken.
var rsiSubtleDegradationClasses = map[string]bool{"imperative-drop": true, "safety-drop": true}

// rsiDispatchSources mirrors coding-dispatch.sh's accepted candidate sources: a
// code candidate from any other source is not yet dispatchable (the runtime-
// error source is deliberately excluded until its allowlist flip).
var rsiDispatchSources = []string{"evolve-tool-gap", "self-harness"}

// RSILoopStatus is the whole recursive-self-improvement snapshot.
type RSILoopStatus struct {
	Layers  []RSILayer
	Turning int // count of layers in LIVE or FROZEN
}

// RSILayer is one loop layer's classified state with display metrics.
type RSILayer struct {
	Key       string
	Title     string
	State     string
	Diagnosis string
	Metrics   []RSIMetricKV
}

// RSIMetricKV is one display metric (label + preformatted value).
type RSIMetricKV struct {
	Label string
	Value string
}

// RSIStatus composes the four layer assessments from the tracker's public
// aggregates. It takes no lock of its own — each aggregate locks internally.
func (t *Tracker) RSIStatus() RSILoopStatus {
	layers := []RSILayer{t.rsiAssessL1(), t.rsiAssessL2(), t.rsiAssessL3(), t.rsiAssessL4()}
	turning := 0
	for _, l := range layers {
		if l.State == RSIStateLive || l.State == RSIStateFrozen {
			turning++
		}
	}
	return RSILoopStatus{Layers: layers, Turning: turning}
}

func (t *Tracker) rsiAssessL1() RSILayer {
	h := t.EvolutionHealth()
	committed := h.Evolves7d + h.Genesis7d
	metrics := []RSIMetricKV{
		{"evolved (7d)", strconv.Itoa(h.Evolves7d)},
		{"new skills", strconv.Itoa(h.Genesis7d)},
		{"rejected", strconv.Itoa(h.EvolveRejected7d)},
		{"confirm rate", fmt.Sprintf("%.0f%%", h.ConfirmRate*100)},
	}
	switch {
	case committed > 0:
		return RSILayer{
			"L1", "skill evolution", RSIStateLive,
			fmt.Sprintf("%d evolved · %d new skills · %d rejected this week", h.Evolves7d, h.Genesis7d, h.EvolveRejected7d), metrics,
		}
	case h.EvolveRejected7d > 0:
		return RSILayer{
			"L1", "skill evolution", RSIStateDataGated,
			"candidates generated but none cleared the gate this week", metrics,
		}
	default:
		return RSILayer{
			"L1", "skill evolution", RSIStateIdle,
			"no skill-evolution activity in the last 7 days", metrics,
		}
	}
}

func (t *Tracker) rsiAssessL2() RSILayer {
	h := t.MetaEvolutionHealth()
	metrics := []RSIMetricKV{
		{"revisions (7d)", strconv.Itoa(h.Revisions7d)},
		{"proposed", strconv.Itoa(h.Proposed7d)},
	}
	if strings.TrimSpace(h.LastEpoch) != "" {
		metrics = append(metrics, RSIMetricKV{"last epoch", h.LastEpoch})
	}
	switch {
	case t.AutoAdoptFrozen():
		return RSILayer{
			"L2", "meta-evolution", RSIStateFrozen,
			"drift self-brake engaged — auto-adopt frozen to propose-only", metrics,
		}
	case h.Revisions7d > 0:
		diag := fmt.Sprintf("%d slow-loop revisions · %d proposed this week", h.Revisions7d, h.Proposed7d)
		if strings.TrimSpace(h.LastEpoch) != "" {
			diag += fmt.Sprintf(" (last: %s)", h.LastEpoch)
		}
		return RSILayer{"L2", "meta-evolution", RSIStateLive, diag, metrics}
	default:
		return RSILayer{
			"L2", "meta-evolution", RSIStateIdle,
			"no slow-loop cycles this week — awaiting the weekly cadence", metrics,
		}
	}
}

func (t *Tracker) rsiAssessL3() RSILayer {
	records, err := t.RecentJudgeAccuracy(20)
	if err != nil || len(records) == 0 {
		return RSILayer{
			"L3", "verifier co-evolution", RSIStateIdle,
			"the judge-accuracy lane has not run yet", nil,
		}
	}
	cutoff := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	runs, misses, falseRejects := 0, 0, 0
	subtleDeployed := false
	for _, r := range records {
		if r.CreatedAt < cutoff {
			continue
		}
		runs++
		misses += len(r.Misses)
		falseRejects += len(r.FalseRejects)
		for cls := range r.ByClass {
			if rsiSubtleDegradationClasses[cls] {
				subtleDeployed = true
			}
		}
	}
	if runs == 0 {
		return RSILayer{
			"L3", "verifier co-evolution", RSIStateIdle,
			"the judge-accuracy lane has not run in the last 7 days", nil,
		}
	}
	metrics := []RSIMetricKV{
		{"lane runs (7d)", strconv.Itoa(runs)},
		{"judge misses", strconv.Itoa(misses)},
		{"false-rejects", strconv.Itoa(falseRejects)},
	}
	switch {
	case misses > 0 || falseRejects > 0:
		return RSILayer{
			"L3", "verifier co-evolution", RSIStateLive,
			fmt.Sprintf("%d judge misses + %d false-rejects over %d runs — P3 fuel accumulating", misses, falseRejects, runs), metrics,
		}
	case !subtleDeployed:
		return RSILayer{
			"L3", "verifier co-evolution", RSIStateDataGated,
			fmt.Sprintf("%d runs; the judge caught every blatant defect and subtle probes are not in the ledger yet", runs), metrics,
		}
	default:
		return RSILayer{
			"L3", "verifier co-evolution", RSIStateDataGated,
			fmt.Sprintf("%d runs with subtle probes but no misses yet — the judge is currently strong", runs), metrics,
		}
	}
}

func (t *Tracker) rsiAssessL4() RSILayer {
	cands, err := t.RecentSelfCorrectionCandidates("", "", 300)
	if err != nil {
		return RSILayer{"L4", "source self-edit", RSIStateIdle, "the candidate store is unavailable", nil}
	}
	byScope := map[string]int{}
	dispatchable := 0
	for _, c := range cands {
		scope := strings.TrimSpace(c.Scope)
		if scope == "" {
			scope = "?"
		}
		byScope[scope]++
		if scope == "code" && normalizeSelfCorrectionStatus(c.Status) == SelfCorrectionStatusProposed && rsiSourceDispatchable(c.Source) {
			dispatchable++
		}
	}
	metrics := []RSIMetricKV{
		{"candidates", strconv.Itoa(len(cands))},
		{"code-scope", strconv.Itoa(byScope["code"])},
		{"dispatchable", strconv.Itoa(dispatchable)},
	}
	switch {
	case dispatchable > 0:
		return RSILayer{
			"L4", "source self-edit", RSIStateLive,
			fmt.Sprintf("%d dispatchable code candidates ready for the coding lane", dispatchable), metrics,
		}
	case len(cands) == 0:
		return RSILayer{"L4", "source self-edit", RSIStateIdle, "no self-correction candidates captured yet", metrics}
	default:
		return RSILayer{
			"L4", "source self-edit", RSIStateStarved,
			fmt.Sprintf("%d candidates (%s) but none are dispatchable code candidates yet", len(cands), rsiScopeSummary(byScope)), metrics,
		}
	}
}

func rsiSourceDispatchable(source string) bool {
	source = strings.TrimSpace(source)
	for _, s := range rsiDispatchSources {
		if strings.HasPrefix(source, s) {
			return true
		}
	}
	return false
}

func rsiScopeSummary(byScope map[string]int) string {
	keys := make([]string, 0, len(byScope))
	for k := range byScope {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%d", k, byScope[k]))
	}
	return strings.Join(parts, ", ")
}
