package regressionwatch

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// watchInterval mirrors the model tuner's cadence — slow enough that a 24h
// telemetry window is mostly fresh each cycle, frequent enough to catch a
// regression within a day.
const watchInterval = 6 * time.Hour

// Deps wires the regression watcher into the gateway.
type Deps struct {
	Sources    []SignalSource
	StatePath  string // baseline JSON path (DENEB_STATE_DIR-aware)
	Thresholds Thresholds
	Logger     *slog.Logger
}

// Task implements autonomous.PeriodicTask (structurally — no import needed).
type Task struct {
	deps Deps
}

// NewTask builds the periodic regression-watch task.
func NewTask(deps Deps) *Task {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Thresholds == (Thresholds{}) {
		deps.Thresholds = DefaultThresholds()
	}
	return &Task{deps: deps}
}

func (t *Task) Name() string            { return "regression-watch" }
func (t *Task) Interval() time.Duration { return watchInterval }

// Run samples every source, detects regressions versus the persisted baseline,
// and logs them. Stage 1 is OBSERVE-ONLY: it never creates a goal. The first
// cycle (or a missing baseline) seeds the baseline instead of detecting, so a
// fresh install never reports phantom regressions against an empty baseline.
func (t *Task) Run(_ context.Context) error {
	current := t.collect()
	if len(current) == 0 {
		return nil
	}
	nowMs := time.Now().UnixMilli()
	base := loadBaseline(t.deps.StatePath)

	if len(base.Entries) == 0 {
		seeded := updateBaseline(base, current, nil, nowMs)
		if err := saveBaseline(t.deps.StatePath, seeded); err != nil {
			return err
		}
		t.deps.Logger.Info("regression-watch: baseline initialized", "signals", len(current))
		return nil
	}

	regs := detect(base, current, t.deps.Thresholds)
	for _, r := range regs {
		// Observe-only: a regression is a Warn, not an action. When the goal
		// path lands (Stage 4), this is where an optimize goal would be enqueued
		// after a dev-bench confirmation pass.
		t.deps.Logger.Warn("regression-watch: regression detected (observe-only)",
			"signal", r.Key,
			"baseline", r.Baseline,
			"current", r.Value,
			"deltaPct", int(r.DeltaPct*100))
	}
	if len(regs) == 0 {
		t.deps.Logger.Debug("regression-watch: no regression", "signals", len(current))
	}

	saved := updateBaseline(base, current, regs, nowMs)
	if err := saveBaseline(t.deps.StatePath, saved); err != nil {
		return err
	}
	return nil
}

// collect samples every source, isolating a panicking adapter so one bad source
// can't kill the whole watch cycle.
func (t *Task) collect() []Signal {
	var out []Signal
	for _, src := range t.deps.Sources {
		out = append(out, t.sampleSource(src)...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullKey() < out[j].FullKey() })
	return out
}

func (t *Task) sampleSource(src SignalSource) (sig []Signal) {
	defer func() {
		if r := recover(); r != nil {
			t.deps.Logger.Error("regression-watch: source panicked", "source", src.Name(), "panic", r)
			sig = nil
		}
	}()
	return src.Sample()
}

func loadBaseline(path string) Baseline {
	var b Baseline
	if path == "" {
		return b
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return b
	}
	_ = json.Unmarshal(raw, &b)
	return b
}

// saveBaseline atomically persists the baseline.
func saveBaseline(path string, b Baseline) error {
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// DefaultStatePath returns the baseline location under the resolved state dir
// (DENEB_STATE_DIR-aware, so a dev gateway never writes the production baseline).
func DefaultStatePath() string {
	return filepath.Join(config.ResolveStateDir(), "regression-baseline.json")
}
