package regressionwatch

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

type fixedLogs struct{ stats []agentlog.ModelStat }

func (f fixedLogs) AggregateByModel(int64) []agentlog.ModelStat { return f.stats }

func latencySignal(t *testing.T, signals []Signal) (Signal, bool) {
	t.Helper()
	for _, s := range signals {
		if s.Key == "agentlog.p95_ms" {
			return s, true
		}
	}
	return Signal{}, false
}

// TestLatencyUsesTheUserFacingPopulation is the whole point of the split.
//
// A p95 over every run measures the largest budget cap in the window, not how
// long anyone waited. Live on 2026-08-27: mixed p95@k3 read 836s while the
// user-facing p95 for the same window was 114s, and the mixed figure was
// reported as a regression all the way into the anomaly ledger.
func TestLatencyUsesTheUserFacingPopulation(t *testing.T) {
	src := AgentLogSource{Logs: fixedLogs{stats: []agentlog.ModelStat{{
		Model: "k3", Runs: 448,
		P95Ms:            836375, // includes 4-hour research crons
		P95MsInteractive: 114000, // what a person actually waited
		InteractiveRuns:  111,
	}}}}
	sig, ok := latencySignal(t, src.Sample())
	if !ok {
		t.Fatal("no latency signal emitted")
	}
	if sig.Value != 114000 {
		t.Errorf("latency value = %v, want the interactive p95 (114000), not the mixed one", sig.Value)
	}
	if sig.Sample != 111 {
		t.Errorf("sample = %d, want the interactive run count — the sample floor must match the population it gates", sig.Sample)
	}
}

// TestNoInteractiveRunsEmitsNoLatencySignal: silence is correct when nobody was
// waiting on anything. A zero would read as "instant".
func TestNoInteractiveRunsEmitsNoLatencySignal(t *testing.T) {
	src := AgentLogSource{Logs: fixedLogs{stats: []agentlog.ModelStat{{
		Model: "k3", Runs: 40, P95Ms: 900000, InteractiveRuns: 0,
	}}}}
	signals := src.Sample()
	if _, ok := latencySignal(t, signals); ok {
		t.Error("a window with no user turns must emit no latency signal")
	}
	// The other signals still ride the full population — an error in an
	// automation lane is still an error.
	var sawRate bool
	for _, s := range signals {
		if s.Key == "agentlog.error_rate" {
			sawRate = true
		}
	}
	if !sawRate {
		t.Error("non-latency signals must keep covering automation lanes")
	}
}
