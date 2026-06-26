package regressionwatch

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSource returns a fixed signal slice each cycle; the test mutates Signals
// between Run calls to simulate telemetry changing.
type fakeSource struct{ Signals []Signal }

func (f *fakeSource) Name() string     { return "fake" }
func (f *fakeSource) Sample() []Signal { return f.Signals }

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// loThresh trips on any rise with no sample/noise gating, so the test can drive
// regressions with small, readable numbers.
func loThresh() Thresholds {
	return Thresholds{RelPct: 0.30, AbsNoiseFloor: 0.0, AbsHard: 0.05, CountHard: 1, MinSample: 0}
}

func TestRun_NotifiesOnNewRegressionAndDedups(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "regression-baseline.json")
	src := &fakeSource{Signals: []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
	}}
	var notified []string
	task := NewTask(Deps{
		Sources:    []SignalSource{src},
		StatePath:  statePath,
		Thresholds: loThresh(),
		Notify: func(_ context.Context, msg string) error {
			notified = append(notified, msg)
			return nil
		},
		Logger: quietLogger(),
	})

	// Cycle 1: seeds the baseline (0.10), never a regression on a fresh install.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 1 (seed): %v", err)
	}
	if len(notified) != 0 {
		t.Fatalf("seed cycle must not notify, got %v", notified)
	}

	// Cycle 2: error rate jumps 0.10 → 0.40 → regression → one notification
	// naming the signal and carrying the delta.
	src.Signals = []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 2 (regress): %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("new regression must notify once, got %d: %v", len(notified), notified)
	}
	if !strings.Contains(notified[0], "agentlog.error_rate@m1") {
		t.Errorf("notification missing signal name: %q", notified[0])
	}
	if !strings.Contains(notified[0], "%") {
		t.Errorf("notification missing a delta percentage: %q", notified[0])
	}

	// Cycle 3: still regressed (the regressed key is held out of the EMA, so the
	// baseline stays 0.10 and it keeps tripping) — but the SET is unchanged, so
	// NO second ping.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run 3 (still regressed): %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("unchanged regression set must not re-notify, got %d", len(notified))
	}
}

func TestRun_ReNotifiesWhenSetChangesAndSilentWhenResolved(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "regression-baseline.json")
	src := &fakeSource{Signals: []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "agentlog.timeout_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
	}}
	var notified []string
	task := NewTask(Deps{
		Sources:    []SignalSource{src},
		StatePath:  statePath,
		Thresholds: loThresh(),
		Notify: func(_ context.Context, msg string) error {
			notified = append(notified, msg)
			return nil
		},
		Logger: quietLogger(),
	})

	// Seed.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// error_rate regresses → notify (set = {error_rate}).
	src.Signals = []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "agentlog.timeout_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("regress error_rate: %v", err)
	}
	if len(notified) != 1 {
		t.Fatalf("first regression must notify, got %d", len(notified))
	}

	// timeout_rate ALSO regresses → set changed {error_rate,timeout_rate} → re-notify.
	src.Signals = []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "agentlog.timeout_rate", Scope: "m1", Value: 0.50, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("regress timeout_rate too: %v", err)
	}
	if len(notified) != 2 {
		t.Fatalf("a changed (larger) regression set must re-notify, got %d", len(notified))
	}
	if !strings.Contains(notified[1], "timeout_rate") {
		t.Errorf("second notification should include the newly regressed signal: %q", notified[1])
	}

	// Both recover → empty set → silence (no "all clear" ping) AND the marker is
	// cleared so a future identical regression set would notify again.
	src.Signals = []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.05, Sample: 100, HigherWorse: true, Kind: KindRate},
		{Key: "agentlog.timeout_rate", Scope: "m1", Value: 0.05, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(notified) != 2 {
		t.Fatalf("resolution must stay silent, got %d", len(notified))
	}
	if got := loadBaseline(statePath).NotifiedFingerprint; got != "" {
		t.Errorf("resolved cycle must clear the notified marker, got %q", got)
	}
}

func TestRun_FingerprintPersistsAcrossRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "regression-baseline.json")
	mk := func(notify func(context.Context, string) error) *Task {
		return NewTask(Deps{
			Sources: []SignalSource{&fakeSource{Signals: []Signal{
				{Key: "agentlog.error_rate", Scope: "m1", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
			}}},
			StatePath:  statePath,
			Thresholds: loThresh(),
			Notify:     notify,
			Logger:     quietLogger(),
		})
	}

	// Seed with a healthy baseline so the next task sees a real regression.
	seed := NewTask(Deps{
		Sources: []SignalSource{&fakeSource{Signals: []Signal{
			{Key: "agentlog.error_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
		}}},
		StatePath: statePath, Thresholds: loThresh(), Logger: quietLogger(),
	})
	if err := seed.Run(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// First task instance detects the regression and notifies.
	var first []string
	if err := mk(func(_ context.Context, m string) error { first = append(first, m); return nil }).Run(context.Background()); err != nil {
		t.Fatalf("run pre-restart: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first instance must notify, got %d", len(first))
	}
	if got := loadBaseline(statePath).NotifiedFingerprint; got == "" {
		t.Fatal("fingerprint should be persisted after a notification")
	}

	// Simulate a gateway restart: a brand-new Task reads the persisted baseline
	// (including the fingerprint). The SAME standing regression must NOT re-ping.
	var second []string
	if err := mk(func(_ context.Context, m string) error { second = append(second, m); return nil }).Run(context.Background()); err != nil {
		t.Fatalf("run post-restart: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("restart must not re-ping a standing regression, got %d: %v", len(second), second)
	}
}

func TestRun_NotifyFailureDoesNotAbortOrAdvanceMarker(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "regression-baseline.json")
	src := &fakeSource{Signals: []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.10, Sample: 100, HigherWorse: true, Kind: KindRate},
	}}
	fail := true
	calls := 0
	task := NewTask(Deps{
		Sources:    []SignalSource{src},
		StatePath:  statePath,
		Thresholds: loThresh(),
		Notify: func(_ context.Context, _ string) error {
			calls++
			if fail {
				return errors.New("relay down")
			}
			return nil
		},
		Logger: quietLogger(),
	})
	if err := task.Run(context.Background()); err != nil { // seed
		t.Fatalf("seed: %v", err)
	}

	src.Signals = []Signal{
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	// Cycle with delivery failing: Run still succeeds (baseline saved), marker
	// stays empty so the next cycle retries the same set.
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("run must not surface a delivery error: %v", err)
	}
	if got := loadBaseline(statePath).NotifiedFingerprint; got != "" {
		t.Errorf("failed delivery must not advance the marker, got %q", got)
	}

	// Next cycle, delivery recovers: same set is retried (because the marker was
	// never set) and now succeeds.
	fail = false
	if err := task.Run(context.Background()); err != nil {
		t.Fatalf("retry cycle: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected a retry after the failed delivery, got %d Notify calls", calls)
	}
	if got := loadBaseline(statePath).NotifiedFingerprint; got == "" {
		t.Errorf("successful retry should set the marker")
	}
}

func TestFingerprint_OrderIndependentAndDistinguishesSets(t *testing.T) {
	a := []Regression{{Key: "b"}, {Key: "a"}}
	b := []Regression{{Key: "a"}, {Key: "b"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Errorf("fingerprint must be order-independent: %q vs %q", Fingerprint(a), Fingerprint(b))
	}
	c := []Regression{{Key: "a"}}
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("different sets must differ")
	}
	if Fingerprint(nil) != "" {
		t.Errorf("empty set fingerprint = %q, want empty", Fingerprint(nil))
	}
}

func TestFormatNotification_IncludesNamesDeltasAndDirection(t *testing.T) {
	msg := formatNotification([]Regression{
		{Key: "agentlog.error_rate@m1", Baseline: 0.10, Value: 0.40, DeltaPct: 3.0, HigherWorse: true},
		{Key: "vllm.cache_hit@m2", Baseline: 0.90, Value: 0.70, DeltaPct: -0.2222, HigherWorse: false},
	})
	for _, want := range []string{"agentlog.error_rate@m1", "vllm.cache_hit@m2", "+300%", "-22%", "0.1", "0.4", "0.9", "0.7"} {
		if !strings.Contains(msg, want) {
			t.Errorf("notification %q missing %q", msg, want)
		}
	}
}
