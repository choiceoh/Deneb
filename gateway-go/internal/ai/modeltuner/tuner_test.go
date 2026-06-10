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
)

type fakeStats struct{ stats []agentlog.ModelStat }

func (f *fakeStats) AggregateByModel(int64) []agentlog.ModelStat { return f.stats }

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
