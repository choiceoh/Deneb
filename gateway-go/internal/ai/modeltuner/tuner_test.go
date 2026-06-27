package modeltuner

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

type fakeStats struct {
	stats  []agentlog.ModelStat
	effort map[string]agentlog.EffortStat
}

func (f *fakeStats) AggregateByModel(int64) []agentlog.ModelStat { return f.stats }

func (f *fakeStats) AggregateEffortByModel(int64) map[string]agentlog.EffortStat {
	return f.effort
}

// tunerRegistry builds a registry with non-vllm roles only, so neither the
// constructor nor the calibration pass performs any network probe.
func tunerRegistry() *modelrole.Registry {
	return modelrole.NewRegistryWithOptions(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		modelrole.RegistryOptions{
			MainModel:        "zai/glm-5-turbo",
			LightweightModel: "zai/glm-5-turbo",
			FallbackModel:    "zai/glm-5-turbo",
			TinyModel:        "zai/glm-5-turbo",
			AnalysisModel:    "zai/glm-5-turbo",
		},
	)
}

func TestAnalyze_Rules(t *testing.T) {
	stats := []agentlog.ModelStat{
		{ // fires fallback + max_tokens + stall + latency (thinking-aware message)
			Model: "m1", Provider: "p", Runs: 10,
			MaxTokensRecoveries: 4, TimeoutRuns: 3, P95Ms: 200_000,
			FallbackRuns: 4, ThinkingRuns: 6,
		},
		{ // fires cache_break (caching active, low read ratio) + tool_errors
			Model: "m2", Provider: "p", Runs: 10,
			CacheCreationTokens: 1000, CacheReadTokens: 100, InputTokens: 5000,
			ToolCalls: 40, ToolErrors: 10,
		},
		{ // below minRuns — never fires regardless of how bad the numbers are
			Model: "m3", Provider: "p", Runs: 2, TimeoutRuns: 2, MaxTokensRecoveries: 2,
		},
		{ // healthy — no recommendations
			Model: "m4", Provider: "p", Runs: 20, P95Ms: 5_000,
			CacheCreationTokens: 100, CacheReadTokens: 9000, InputTokens: 1000,
		},
	}
	recs := Analyze(stats)

	got := map[string][]string{}
	for _, r := range recs {
		got[r.Model] = append(got[r.Model], r.Rule)
	}
	if want := []string{"fallback", "latency", "max_tokens", "stall"}; strings.Join(got["m1"], ",") != strings.Join(want, ",") {
		t.Errorf("m1 rules = %v, want %v (sorted)", got["m1"], want)
	}
	if want := []string{"cache_break", "tool_errors"}; strings.Join(got["m2"], ",") != strings.Join(want, ",") {
		t.Errorf("m2 rules = %v, want %v", got["m2"], want)
	}
	if len(got["m3"]) != 0 || len(got["m4"]) != 0 {
		t.Errorf("m3/m4 must not fire: %v / %v", got["m3"], got["m4"])
	}
	// The max_tokens rule must carry the auto-apply floor.
	for _, r := range recs {
		if r.Rule == "max_tokens" && r.TunedMaxTokens != tunedMaxTokensFloor {
			t.Errorf("max_tokens rec floor = %d, want %d", r.TunedMaxTokens, tunedMaxTokensFloor)
		}
	}
}

func TestFingerprint_IgnoresMessageDrift(t *testing.T) {
	a := []Recommendation{{Model: "m", Provider: "p", Rule: "stall", Message: "3/10런"}}
	b := []Recommendation{{Model: "m", Provider: "p", Rule: "stall", Message: "4/12런"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("message-only drift must not change the fingerprint")
	}
	c := []Recommendation{{Model: "m", Provider: "p", Rule: "latency"}}
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("different rules must change the fingerprint")
	}
}

func TestTask_Run_AppliesAndClearsTunedFloor(t *testing.T) {
	reg := tunerRegistry()
	statePath := filepath.Join(t.TempDir(), "model-stats.json")
	src := &fakeStats{stats: []agentlog.ModelStat{
		{Model: "m1", Provider: "p", Runs: 10, MaxTokensRecoveries: 5},
	}}
	var notified []string
	task := NewTask(Deps{
		Logs: src, Registry: reg, StatePath: statePath,
		Notify: func(_ context.Context, msg string) error {
			notified = append(notified, msg)
			return nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Cycle 1: truncation rule fires → floor applied + notification sent.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if got := reg.TunedMaxTokens("m1"); got != tunedMaxTokensFloor {
		t.Fatalf("tuned floor = %d, want %d", got, tunedMaxTokensFloor)
	}
	if len(notified) != 1 || !strings.Contains(notified[0], "m1") {
		t.Fatalf("notification = %v, want one mentioning m1", notified)
	}

	// Cycle 2: same situation → fingerprint unchanged → no second ping.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("unchanged situation must not re-notify, got %d messages", len(notified))
	}

	// Cycle 3: model recovered → floor cleared, silence (no "all clear" ping).
	src.stats = []agentlog.ModelStat{{Model: "m1", Provider: "p", Runs: 10}}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	if got := reg.TunedMaxTokens("m1"); got != 0 {
		t.Fatalf("recovered model floor = %d, want cleared", got)
	}
	if len(notified) != 1 {
		t.Fatalf("resolution must stay silent, got %d messages", len(notified))
	}

	// Scorecard persisted and reloadable.
	sc := LoadScorecard(statePath)
	if sc.GeneratedAtMs == 0 || len(sc.Models) != 1 {
		t.Fatalf("scorecard not persisted: %+v", sc)
	}
}

func TestApplyEffortNudge_OptInAndApply(t *testing.T) {
	reg := tunerRegistry()
	task := &Task{deps: Deps{Registry: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}}

	// A high-escalation signal over a sufficient sample for the served model:
	// 5/20 routed-off runs restored thinking (0.25 > 0.15), 30 total runs.
	stats := map[string]agentlog.EffortStat{
		"glm-5-turbo": {RoutedRuns: 20, KeptRuns: 10, EscalatedRuns: 5},
		// An empty-model bucket must be skipped (can't be attributed to a gate).
		"": {RoutedRuns: 50, KeptRuns: 50, EscalatedRuns: 40},
	}
	base := reg.RoutingProfileForModel("zai", "glm-5-turbo").MaxSimpleRunes

	// Flag off (default): no-op, gate untouched.
	t.Setenv("DENEB_ADAPTIVE_EFFORT_TUNE", "")
	if n := task.applyEffortNudge(stats); n != 0 {
		t.Fatalf("flag off must nudge nothing, got %d", n)
	}
	if reg.TunedMaxSimpleRunes("glm-5-turbo") != 0 {
		t.Fatalf("flag off must not write a tuned gate, got %d", reg.TunedMaxSimpleRunes("glm-5-turbo"))
	}

	// Flag on: the high-escalation gate steps DOWN by one step (stricter).
	t.Setenv("DENEB_ADAPTIVE_EFFORT_TUNE", "1")
	if n := task.applyEffortNudge(stats); n != 1 {
		t.Fatalf("flag on must nudge exactly the one attributable model, got %d", n)
	}
	want := base - router.EffortNudgeStep
	if got := reg.TunedMaxSimpleRunes("glm-5-turbo"); got != want {
		t.Fatalf("tuned gate = %d, want %d (one step below %d)", got, want, base)
	}
	// The empty-model bucket left no tuned gate behind.
	if got := reg.TunedMaxSimpleRunes(""); got != 0 {
		t.Fatalf("empty-model bucket must not be tuned, got %d", got)
	}

	// The live read path now reflects the nudged gate.
	if got := reg.RoutingProfileForModel("zai", "glm-5-turbo").MaxSimpleRunes; got != want {
		t.Fatalf("resolved gate after nudge = %d, want %d", got, want)
	}

	// A second cycle with a now-healthy signal must not move the gate.
	healthy := map[string]agentlog.EffortStat{
		"glm-5-turbo": {RoutedRuns: 12, KeptRuns: 18, EscalatedRuns: 1},
	}
	if n := task.applyEffortNudge(healthy); n != 0 {
		t.Fatalf("healthy signal must not nudge, got %d", n)
	}
	if got := reg.TunedMaxSimpleRunes("glm-5-turbo"); got != want {
		t.Fatalf("healthy cycle moved the gate to %d, want it to stay %d", got, want)
	}
}

func TestKoreanRatio(t *testing.T) {
	if r := koreanRatio("안녕하세요 저는 데네브입니다"); r < 0.9 {
		t.Errorf("korean text ratio = %f, want ~1", r)
	}
	if r := koreanRatio("hello world"); r != 0 {
		t.Errorf("english text ratio = %f, want 0", r)
	}
	if r := koreanRatio("123 !!"); r != 0 {
		t.Errorf("no-letter ratio = %f, want 0", r)
	}
}

func TestNewTask_ReappliesPersistedFloors(t *testing.T) {
	reg := tunerRegistry()
	statePath := filepath.Join(t.TempDir(), "model-stats.json")

	// Persist a scorecard carrying a floor recommendation, as a prior cycle
	// would have, then simulate a restart by building a fresh task.
	err := saveScorecard(statePath, Scorecard{
		GeneratedAtMs: 1,
		Recommendations: []Recommendation{
			{Model: "m1", Provider: "p", Rule: "max_tokens", TunedMaxTokens: tunedMaxTokensFloor},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	NewTask(Deps{
		Logs: &fakeStats{}, Registry: reg, StatePath: statePath,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if got := reg.TunedMaxTokens("m1"); got != tunedMaxTokensFloor {
		t.Fatalf("floor after restart = %d, want %d re-applied from scorecard", got, tunedMaxTokensFloor)
	}
}

func TestScorecardNoteAndAdvisories(t *testing.T) {
	sc := Scorecard{
		WindowHours: 24,
		Models: []agentlog.ModelStat{{
			Model: "gemma4", Provider: "vllm", Runs: 12, P95Ms: 8000,
			CacheReadTokens: 9000, CacheCreationTokens: 100, InputTokens: 1000,
			FallbackRuns: 2, TimeoutRuns: 1,
		}},
		Calibrations: map[string]Calibration{
			"gemma4": {Model: "gemma4", LatencyMs: 1200, KoreanOK: true},
		},
		Recommendations: []Recommendation{
			{Model: "gemma4", Provider: "vllm", Rule: "stall", Message: "스톨 점검"},
		},
	}

	note := sc.NoteFor("gemma4", 16384)
	for _, want := range []string{"24h 12런", "p95 8초", "캐시 90%", "폴백 2", "스톨 1", "프로브 1.2초✓", "출력 floor 16384"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q missing %q", note, want)
		}
	}
	if got := sc.NoteFor("unknown-model", 0); got != "" {
		t.Errorf("unknown model note = %q, want empty", got)
	}

	lines := sc.AdvisoryLines()
	if len(lines) != 1 || !strings.Contains(lines[0], "vllm/gemma4: 스톨 점검") {
		t.Errorf("advisories = %v", lines)
	}
}
