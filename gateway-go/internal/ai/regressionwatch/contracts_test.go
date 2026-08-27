package regressionwatch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

func TestDefaultThresholdsContract(t *testing.T) {
	got := DefaultThresholds()
	want := Thresholds{RelPct: 0.30, AbsNoiseFloor: 0.02, AbsHard: 0.10, CountHard: 5, MinSample: 20}
	if got != want {
		t.Fatalf("DefaultThresholds = %+v, want %+v", got, want)
	}
}

func TestSignalFullKeyContract(t *testing.T) {
	for _, tt := range []struct {
		signal Signal
		want   string
	}{
		{signal: Signal{Key: "key"}, want: "key"},
		{signal: Signal{Key: "key", Scope: "model"}, want: "key@model"},
		{signal: Signal{Scope: "model"}, want: "@model"},
		{signal: Signal{}},
	} {
		if got := tt.signal.FullKey(); got != tt.want {
			t.Errorf("FullKey(%+v) = %q, want %q", tt.signal, got, tt.want)
		}
	}
}

func TestRateContract(t *testing.T) {
	for _, tt := range []struct {
		n, d int
		want float64
	}{
		{n: 0, d: 0},
		{n: 1, d: 0},
		{n: 1, d: -1},
		{n: 0, d: 10},
		{n: 1, d: 4, want: 0.25},
		{n: 5, d: 2, want: 2.5},
		{n: -1, d: 2, want: -0.5},
	} {
		if got := rate(tt.n, tt.d); got != tt.want {
			t.Errorf("rate(%d,%d) = %v, want %v", tt.n, tt.d, got, tt.want)
		}
	}
}

func TestRegressedBoundaryMatrix(t *testing.T) {
	th := DefaultThresholds()
	for _, tt := range []struct {
		name      string
		base, cur float64
		higher    bool
		kind      SignalKind
		floor     float64
		want      bool
	}{
		{name: "flat", base: 1, cur: 1, higher: true, kind: KindRate},
		{name: "improved high worse", base: 1, cur: .5, higher: true, kind: KindRate},
		{name: "improved low worse", base: .5, cur: 1, higher: false, kind: KindRate},
		{name: "rate relative and noise edge", base: .1, cur: .13, higher: true, kind: KindRate, want: true},
		{name: "rate relative below noise", base: .01, cur: .013, higher: true, kind: KindRate},
		{name: "rate hard floor", base: .5, cur: .61, higher: true, kind: KindRate, want: true},
		{name: "low worse hard", base: .9, cur: .79, higher: false, kind: KindRate, want: true},
		{name: "scalar relative edge", base: 100, cur: 130, higher: true, kind: KindScalar, want: true},
		{name: "scalar zero base", base: 0, cur: 100, higher: true, kind: KindScalar},
		{name: "count default below", base: 0, cur: 4, higher: true, kind: KindCount},
		{name: "count default edge", base: 0, cur: 5, higher: true, kind: KindCount, want: true},
		{name: "count custom", base: 0, cur: 2, higher: true, kind: KindCount, floor: 2, want: true},
		{name: "count lower worse", base: 10, cur: 8, higher: false, kind: KindCount, floor: 2, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := regressed(tt.base, tt.cur, tt.higher, tt.kind, tt.floor, th); got != tt.want {
				t.Fatalf("regressed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRelDeltaContract(t *testing.T) {
	for _, tt := range []struct{ base, cur, want float64 }{
		{base: 0, cur: 1, want: 0},
		{base: 1e-10, cur: 1, want: 0},
		{base: 10, cur: 15, want: .5},
		{base: 10, cur: 5, want: -.5},
		{base: -10, cur: -5, want: .5},
	} {
		if got := relDelta(tt.base, tt.cur); math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("relDelta(%v,%v) = %v, want %v", tt.base, tt.cur, got, tt.want)
		}
	}
}

func TestDetectContractAdditional(t *testing.T) {
	base := Baseline{Entries: map[string]BaselineEntry{
		"rate@m": {Value: .1},
		"count":  {Value: 0},
		"low@m":  {Value: .9},
	}}
	current := []Signal{
		{Key: "rate", Scope: "m", Value: .3, Sample: 20, HigherWorse: true, Kind: KindRate},
		{Key: "count", Value: 1, Sample: 0, HigherWorse: true, Kind: KindCount, HardFloor: 1},
		{Key: "low", Scope: "m", Value: .6, Sample: 20, HigherWorse: false, Kind: KindRate},
		{Key: "new", Value: 100, Sample: 100, HigherWorse: true, Kind: KindScalar},
	}
	got := detect(base, current, DefaultThresholds())
	if len(got) != 3 {
		t.Fatalf("regressions = %+v", got)
	}
	for i, key := range []string{"rate@m", "count", "low@m"} {
		if got[i].Key != key {
			t.Errorf("got[%d] = %+v", i, got[i])
		}
	}
	if got[2].DeltaPct >= 0 || got[2].HigherWorse {
		t.Fatalf("lower-is-worse regression = %+v", got[2])
	}
}

func TestFingerprintContract(t *testing.T) {
	if got := Fingerprint(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	regs := []Regression{{Key: "z"}, {Key: "a"}, {Key: "z"}}
	if got := Fingerprint(regs); got != "a,z,z" {
		t.Fatalf("fingerprint = %q", got)
	}
	if regs[0].Key != "z" {
		t.Fatal("Fingerprint mutated input")
	}
}

func TestFormatNotificationContract(t *testing.T) {
	got := formatNotification([]Regression{
		{Key: "error@m", Baseline: .1, Value: .2, DeltaPct: 1, HigherWorse: true},
		{Key: "cache@m", Baseline: .9, Value: .7, DeltaPct: -.222, HigherWorse: false},
	})
	for _, want := range []string{"⚠️ 회귀 감지", "error@m: 0.1 → 0.2 (+100%)", "cache@m: 0.9 → 0.7 (-22%)", "~/.deneb/regression-baseline.json"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

func TestUpdateBaselineCountZeroSampleAndImmutability(t *testing.T) {
	base := Baseline{GeneratedAtMs: 1, NotifiedFingerprint: "standing", Entries: map[string]BaselineEntry{
		"rate":      {Value: .2, Sample: 100, UpdatedAtMs: 1},
		"protected": {Value: .1, Sample: 100, UpdatedAtMs: 1},
	}}
	current := []Signal{
		{Key: "rate", Value: .5, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "protected", Value: .9, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "count", Value: 1, Sample: 0, HigherWorse: true, Kind: KindCount},
		{Key: "empty-rate", Value: .5, Sample: 0, HigherWorse: true, Kind: KindRate},
	}
	got := updateBaseline(base, current, []Regression{{Key: "protected"}}, 10)
	if got.GeneratedAtMs != 10 || math.Abs(got.Entries["rate"].Value-.29) > 1e-12 {
		t.Fatalf("updated = %+v", got)
	}
	if got.Entries["protected"].Value != .1 {
		t.Fatalf("protected absorbed = %+v", got.Entries["protected"])
	}
	if got.Entries["count"].Value != 1 || got.Entries["count"].Sample != 0 {
		t.Fatalf("count not seeded = %+v", got.Entries["count"])
	}
	if _, ok := got.Entries["empty-rate"]; ok {
		t.Fatal("zero-sample rate seeded")
	}
	if base.Entries["rate"].Value != .2 || len(base.Entries) != 2 {
		t.Fatalf("input mutated = %+v", base)
	}
}

type fakeAgentStats struct {
	stats []agentlog.ModelStat
	since int64
}

func (f *fakeAgentStats) AggregateByModel(since int64) []agentlog.ModelStat {
	f.since = since
	return f.stats
}

func TestAgentLogSourceContract(t *testing.T) {
	if (AgentLogSource{}).Name() != "agentlog" {
		t.Fatal("Name")
	}
	if got := (AgentLogSource{}).Sample(); got != nil {
		t.Fatalf("nil logs = %+v", got)
	}
	logs := &fakeAgentStats{stats: []agentlog.ModelStat{
		// P95Ms is the mixed-population figure and P95MsInteractive the
		// user-facing one; the latency signal must carry the latter. A window
		// with no interactive runs emits no latency signal at all, which is why
		// InteractiveRuns is part of the fixture rather than implied.
		{
			Model: "m", Runs: 10, Errors: 2, TimeoutRuns: 1, MaxTokensRecoveries: 3,
			P95Ms: 9999, P95MsInteractive: 1234, InteractiveRuns: 6,
			ToolCalls: 4, ToolErrors: 1,
		},
		{Model: "empty"},
	}}
	before := time.Now().Add(-2 * time.Hour).UnixMilli()
	got := (AgentLogSource{Logs: logs, Window: 2 * time.Hour}).Sample()
	after := time.Now().Add(-2 * time.Hour).UnixMilli()
	if logs.since < before || logs.since > after || len(got) != 5 {
		t.Fatalf("since=%d range=%d..%d signals=%+v", logs.since, before, after, got)
	}
	for _, sig := range got {
		if sig.Key != "agentlog.p95_ms" {
			continue
		}
		if sig.Value != 1234 {
			t.Errorf("latency value = %v, want the interactive p95 (1234), not the mixed 9999", sig.Value)
		}
		if sig.Sample != 6 {
			t.Errorf("latency sample = %d, want the interactive run count", sig.Sample)
		}
	}
	wantKeys := []string{"agentlog.error_rate@m", "agentlog.timeout_rate@m", "agentlog.max_tokens_rate@m", "agentlog.p95_ms@m", "agentlog.tool_error_rate@m"}
	for i, key := range wantKeys {
		if got[i].FullKey() != key {
			t.Errorf("signal %d = %+v", i, got[i])
		}
	}
	if got[0].Value != 2.0/12.0 || got[4].Value != .25 {
		t.Fatalf("rates = %+v", got)
	}
}

type fakeHealthRegistry struct {
	models    map[modelrole.Role]modelrole.ModelConfig
	unhealthy map[string]bool
}

func (f fakeHealthRegistry) ConfiguredModels() map[modelrole.Role]modelrole.ModelConfig {
	return f.models
}
func (f fakeHealthRegistry) ModelUnhealthy(model string) bool { return f.unhealthy[model] }

func TestHealthSourceContract(t *testing.T) {
	if (HealthSource{}).Name() != "model-health" {
		t.Fatal("Name")
	}
	if got := (HealthSource{}).Sample(); got != nil {
		t.Fatalf("nil registry = %+v", got)
	}
	reg := fakeHealthRegistry{
		models: map[modelrole.Role]modelrole.ModelConfig{
			modelrole.RoleMain:        {Model: "main"},
			modelrole.RoleCoding:      {Model: "main"},
			modelrole.RoleTiny:        {Model: "fast"},
			modelrole.RoleLightweight: {},
		},
		unhealthy: map[string]bool{"main": true},
	}
	got := (HealthSource{Registry: reg}).Sample()
	if len(got) != 2 {
		t.Fatalf("signals = %+v", got)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].Scope < got[j].Scope })
	if got[0].Scope != "fast" || got[0].Value != 0 || got[1].Scope != "main" || got[1].Value != 1 {
		t.Fatalf("signals = %+v", got)
	}
	for _, s := range got {
		if s.Kind != KindCount || s.HardFloor != 1 || s.Sample != 1 || !s.HigherWorse {
			t.Errorf("shape = %+v", s)
		}
	}
}

type contractSource struct {
	name       string
	signals    []Signal
	panicValue any
}

func (s contractSource) Name() string { return s.name }
func (s contractSource) Sample() []Signal {
	if s.panicValue != nil {
		panic(s.panicValue)
	}
	return s.signals
}

func TestTaskDefaultsAndCollectRecoversFromPanickingSource(t *testing.T) {
	task := NewTask(Deps{})
	if task.Name() != "regression-watch" || task.Interval() != 6*time.Hour || task.deps.Logger == nil || task.deps.Thresholds != DefaultThresholds() {
		t.Fatalf("task = %+v", task)
	}
	task = NewTask(Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sources: []SignalSource{
		nil,
		contractSource{name: "z", signals: []Signal{{Key: "z"}}},
		contractSource{name: "panic", panicValue: "boom"},
		contractSource{name: "a", signals: []Signal{{Key: "a", Scope: "2"}, {Key: "a", Scope: "1"}}},
	}})
	got := task.collect()
	keys := make([]string, len(got))
	for i := range got {
		keys[i] = got[i].FullKey()
	}
	if !reflect.DeepEqual(keys, []string{"a@1", "a@2", "z"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestBaselineLoadSaveContracts(t *testing.T) {
	if got := loadBaseline(""); got.Entries != nil || got.GeneratedAtMs != 0 {
		t.Fatalf("empty load = %+v", got)
	}
	if err := saveBaseline("", Baseline{}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	want := Baseline{GeneratedAtMs: 10, NotifiedFingerprint: "a", Entries: map[string]BaselineEntry{"a": {Value: .5, Sample: 10, HigherWorse: true, UpdatedAtMs: 10}}}
	if err := saveBaseline(path, want); err != nil {
		t.Fatal(err)
	}
	if got := loadBaseline(path); !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["generatedAtMs"] != float64(10) {
		t.Fatalf("raw = %#v", raw)
	}
	if got := loadBaseline(filepath.Join(t.TempDir(), "missing")); got.Entries != nil {
		t.Fatalf("missing = %+v", got)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadBaseline(bad); got.Entries != nil {
		t.Fatalf("bad = %+v", got)
	}
}

func TestTaskRunSeedRegressionNotifyDedupAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	source := &fakeSource{Signals: []Signal{{Key: "rate", Value: .1, Sample: 100, HigherWorse: true, Kind: KindRate}}}
	var notifications []string
	failNotify := false
	task := NewTask(Deps{
		Sources: []SignalSource{source}, StatePath: path, Thresholds: Thresholds{RelPct: .3, AbsNoiseFloor: .01, AbsHard: .1}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Notify: func(_ context.Context, msg string) error {
			if failNotify {
				return errors.New("offline")
			}
			notifications = append(notifications, msg)
			return nil
		},
	})
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 0 {
		t.Fatal("seed notified")
	}
	source.Signals[0].Value = .3
	failNotify = true
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 0 || loadBaseline(path).NotifiedFingerprint != "" {
		t.Fatal("failed notify marked delivered")
	}
	failNotify = false
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 1 || loadBaseline(path).NotifiedFingerprint != "rate" {
		t.Fatalf("notification = %v base=%+v", notifications, loadBaseline(path))
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(notifications) != 1 {
		t.Fatal("standing regression repeated")
	}
	source.Signals[0].Value = .05
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loadBaseline(path).NotifiedFingerprint != "" {
		t.Fatal("recovery did not clear fingerprint")
	}
}

func TestTaskRunNoSignalsAndSaveFailure(t *testing.T) {
	task := NewTask(Deps{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(rootFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	task = NewTask(Deps{StatePath: filepath.Join(rootFile, "baseline.json"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sources: []SignalSource{contractSource{name: "one", signals: []Signal{{Key: "x", Value: 1, Sample: 1}}}}})
	if err := task.Run(context.Background()); err == nil {
		t.Fatal("save failure swallowed")
	}
}

func TestDefaultStatePathReturnsResolvedStateDir(t *testing.T) {
	path := DefaultStatePath()
	if filepath.Base(path) != "regression-baseline.json" || filepath.Dir(path) == "." {
		t.Fatalf("path = %q", path)
	}
}
