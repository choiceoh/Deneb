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
	// Notify, when set, pushes a proactive operator notification on a newly
	// detected regression set (regressed signal names + deltas). It reuses the
	// gateway's native operator relay; a nil error means delivered (or
	// delivery-not-wired no-op). Optional — when nil the watcher stays
	// log-only. The call is de-duped by the persisted NotifiedFingerprint so a
	// standing regression pings once, not every cycle.
	Notify func(ctx context.Context, msg string) error
	Logger *slog.Logger
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
func (t *Task) Run(ctx context.Context) error {
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
		// Observe-only on the GOAL path: a regression is a Warn, not an
		// optimize goal. When the goal path lands (Stage 4), this is where an
		// optimize goal would be enqueued after a dev-bench confirmation pass.
		// Until then the safe, valuable close is a proactive operator
		// notification (emitted below) IN ADDITION to this Warn.
		t.deps.Logger.Warn("regression-watch: regression detected (observe-only)",
			"signal", r.Key,
			"baseline", r.Baseline,
			"current", r.Value,
			"deltaPct", int(r.DeltaPct*100))
	}
	if len(regs) == 0 {
		t.deps.Logger.Debug("regression-watch: no regression", "signals", len(current))
	}

	// Notify the operator, but only when the regressed SET changed since the
	// last notification — a standing regression keeps tripping the detector
	// every cycle (its key is held out of the baseline EMA), so without this
	// de-dup the operator would be pinged every 6h. The fingerprint is carried
	// on the persisted baseline so a gateway restart doesn't re-ping.
	//   - empty set: nothing to send, and clear the marker so the NEXT distinct
	//     regression notifies even if it happens to repeat an earlier set.
	fp := Fingerprint(regs)
	if t.deps.Notify != nil && len(regs) > 0 && fp != base.NotifiedFingerprint {
		if err := t.deps.Notify(ctx, formatNotification(regs)); err != nil {
			// Delivery failure must not abort the cycle or lose the baseline
			// update — log and fall through, leaving the marker unchanged so the
			// next cycle retries the same set.
			t.deps.Logger.Warn("regression-watch: notify failed", "error", err)
		} else {
			base.NotifiedFingerprint = fp
		}
	} else if len(regs) == 0 {
		base.NotifiedFingerprint = ""
	}

	saved := updateBaseline(base, current, regs, nowMs)
	saved.NotifiedFingerprint = base.NotifiedFingerprint
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
