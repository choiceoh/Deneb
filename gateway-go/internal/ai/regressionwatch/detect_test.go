package regressionwatch

import "testing"

func TestRegressed_DirectionAndFloors(t *testing.T) {
	th := Thresholds{RelPct: 0.30, AbsNoiseFloor: 0.02, AbsHard: 0.10, CountHard: 5, MinSample: 0}
	cases := []struct {
		name        string
		base, cur   float64
		higherWorse bool
		kind        SignalKind
		hardFloor   float64
		want        bool
	}{
		{"rate up past floors", 0.10, 0.20, true, KindRate, 0, true},
		{"rate up but tiny absolute", 0.001, 0.0015, true, KindRate, 0, false}, // +50% rel, below noise floor
		{"rate up but tiny relative", 0.50, 0.52, true, KindRate, 0, false},    // +0.02 abs, <30% rel and <hard
		{"rate flat", 0.10, 0.10, true, KindRate, 0, false},
		{"rate improved", 0.20, 0.10, true, KindRate, 0, false},
		{"cache hit fell — hard absolute", 0.90, 0.70, false, KindRate, 0, true}, // 22% rel < 30%, but 0.20 ≥ hard
		{"cache hit rose", 0.70, 0.90, false, KindRate, 0, false},
		{"scalar latency up 40pct", 1000, 1400, true, KindScalar, 0, true},
		{"scalar latency up 10pct", 1000, 1100, true, KindScalar, 0, false},
		{"count from zero past default floor", 0, 6, true, KindCount, 0, true},   // 6 ≥ CountHard 5
		{"count from zero below default floor", 0, 3, true, KindCount, 0, false}, // 3 < 5
		{"count binary flip with HardFloor 1", 0, 1, true, KindCount, 1, true},   // circuit opened
		{"count flat (already open)", 1, 1, true, KindCount, 1, false},           // no new flip
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := regressed(tc.base, tc.cur, tc.higherWorse, tc.kind, tc.hardFloor, th); got != tc.want {
				t.Errorf("regressed(%v→%v, hw=%v, kind=%v, floor=%v) = %v, want %v",
					tc.base, tc.cur, tc.higherWorse, tc.kind, tc.hardFloor, got, tc.want)
			}
		})
	}
}

func TestDetect_SkipsNoBaselineAndLowSample(t *testing.T) {
	base := Baseline{Entries: map[string]BaselineEntry{
		"agentlog.error_rate@m1": {Value: 0.10, HigherWorse: true},
	}}
	th := Thresholds{RelPct: 0.30, AbsNoiseFloor: 0.02, AbsHard: 0.10, MinSample: 20}
	current := []Signal{
		// regressed, enough samples → detected
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.30, Sample: 100, HigherWorse: true, Kind: KindRate},
		// regressed, but too few samples → skipped
		{Key: "agentlog.error_rate", Scope: "m1", Value: 0.30, Sample: 5, HigherWorse: true, Kind: KindRate},
		// no baseline yet → skipped
		{Key: "agentlog.timeout_rate", Scope: "m2", Value: 0.40, Sample: 100, HigherWorse: true, Kind: KindRate},
	}
	// First element has enough samples; the low-sample dup would also match the
	// same key, so detect on just the first to keep the assertion crisp.
	got := detect(base, current[:1], th)
	if len(got) != 1 || got[0].Key != "agentlog.error_rate@m1" {
		t.Fatalf("expected 1 regression on m1, got %+v", got)
	}

	if r := detect(base, current[1:2], th); len(r) != 0 {
		t.Errorf("low-sample signal should be skipped, got %+v", r)
	}
	if r := detect(base, current[2:3], th); len(r) != 0 {
		t.Errorf("no-baseline signal should be skipped, got %+v", r)
	}
}

func TestUpdateBaseline_SeedsEMAsAndProtectsRegressions(t *testing.T) {
	// Seed: empty baseline takes current values verbatim.
	seeded := updateBaseline(Baseline{}, []Signal{
		{Key: "k", Scope: "m", Value: 0.10, Sample: 100, HigherWorse: true},
	}, nil, 1000)
	if got := seeded.Entries["k@m"].Value; got != 0.10 {
		t.Fatalf("seed should be verbatim, got %v", got)
	}

	// EMA: a non-regressed key moves a fraction toward the new value.
	moved := updateBaseline(seeded, []Signal{
		{Key: "k", Scope: "m", Value: 0.20, Sample: 100, HigherWorse: true},
	}, nil, 2000)
	if got := moved.Entries["k@m"].Value; got <= 0.10 || got >= 0.20 {
		t.Fatalf("EMA should land strictly between old and new, got %v", got)
	}

	// Protected: a regressed key keeps its old baseline (not absorbed).
	protected := updateBaseline(seeded, []Signal{
		{Key: "k", Scope: "m", Value: 0.50, Sample: 100, HigherWorse: true},
	}, []Regression{{Key: "k@m"}}, 3000)
	if got := protected.Entries["k@m"].Value; got != 0.10 {
		t.Errorf("regressed key must keep its baseline, got %v", got)
	}
}
